#!/usr/bin/env python3
"""Validate AF Stack deploy target manifests without cloud credentials."""

from __future__ import annotations

import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import tomllib
from typing import Any

ROOT = pathlib.Path(__file__).resolve().parents[1]


class CheckError(Exception):
    pass


def rel(path: pathlib.Path | str) -> pathlib.Path:
    return ROOT / path


def require_file(path: pathlib.Path | str) -> pathlib.Path:
    target = rel(path)
    if not target.is_file():
        raise CheckError(f"missing file: {target.relative_to(ROOT)}")
    return target


def run(cmd: list[str], *, cwd: pathlib.Path = ROOT) -> subprocess.CompletedProcess[str]:
    return subprocess.run(cmd, cwd=cwd, text=True, capture_output=True, check=False)  # noqa: S603 - runs fixed deploy CLIs


def require(condition: bool, message: str) -> None:
    if not condition:
        raise CheckError(message)


def find_service(services: list[dict[str, Any]], name: str) -> dict[str, Any]:
    for service in services:
        if service.get("name") == name:
            return service
    raise CheckError(f"missing Railway service {name!r}")


def check_helm() -> list[str]:
    messages: list[str] = []
    chart = require_file("deploy/helm/af-stack/Chart.yaml").parent
    require_file("deploy/helm/af-stack/values.yaml")
    require_file("deploy/helm/af-stack/values-dev.yaml")
    require_file("deploy/helm/af-stack/values-prod.yaml")

    runtime_deploy = require_file(
        "deploy/helm/af-stack/templates/runtime/deployment.yaml"
    ).read_text()
    dashboard_deploy = require_file(
        "deploy/helm/af-stack/templates/dashboard/deployment.yaml"
    ).read_text()
    values = rel("deploy/helm/af-stack/values.yaml").read_text()
    require("/health" in runtime_deploy, "runtime Deployment must define /health liveness probe")
    require("/ready" in runtime_deploy, "runtime Deployment must define /ready readiness probe")
    require("/" in dashboard_deploy, "dashboard Deployment must define root health probe")
    require("service: runtime" in values, "Helm values must route runtime paths")
    require(
        "/health" in values and "/ready" in values, "Helm values must route runtime health paths"
    )

    helm = shutil.which("helm")
    if not helm:
        messages.append("helm binary not found; skipped helm lint/template")
        return messages

    lint = run([helm, "lint", str(chart), "-f", str(chart / "values-dev.yaml")])
    if lint.returncode != 0:
        raise CheckError(f"helm lint failed:\n{lint.stdout}\n{lint.stderr}")

    template = run(
        [
            helm,
            "template",
            "af-stack",
            str(chart),
            "-f",
            str(chart / "values-dev.yaml"),
            "--set",
            "ingress.enabled=false",
        ]
    )
    if template.returncode != 0:
        raise CheckError(f"helm template failed:\n{template.stdout}\n{template.stderr}")
    rendered = template.stdout
    for needle in (
        "kind: Deployment",
        "name: af-stack-runtime",
        "name: af-stack-dashboard",
        "path: /ready",
    ):
        require(needle in rendered, f"helm template output missing {needle!r}")
    messages.append("helm lint/template passed")
    return messages


def check_fly() -> list[str]:
    messages: list[str] = []
    runtime_path = require_file("deploy/fly/fly.toml")
    dashboard_path = require_file("deploy/fly/fly.dashboard.toml")
    runtime = tomllib.loads(runtime_path.read_text())
    dashboard = tomllib.loads(dashboard_path.read_text())

    require(
        runtime.get("build", {}).get("dockerfile") == "services/runtime/Dockerfile",
        "Fly runtime Dockerfile mismatch",
    )
    require(
        dashboard.get("build", {}).get("dockerfile") == "apps/dashboard/Dockerfile",
        "Fly dashboard Dockerfile mismatch",
    )

    runtime_services = runtime.get("services", [])
    require(
        runtime_services and runtime_services[0].get("internal_port") == 8080,
        "Fly runtime must expose internal port 8080",
    )
    runtime_checks = runtime_services[0].get("http_checks", [])
    runtime_paths = {check.get("path") for check in runtime_checks}
    require(
        {"/health", "/ready"}.issubset(runtime_paths),
        "Fly runtime must define /health and /ready checks",
    )

    dashboard_services = dashboard.get("services", [])
    require(
        dashboard_services and dashboard_services[0].get("internal_port") == 3000,
        "Fly dashboard must expose internal port 3000",
    )
    dashboard_paths = {check.get("path") for check in dashboard_services[0].get("http_checks", [])}
    require("/" in dashboard_paths, "Fly dashboard must define root health check")

    flyctl = shutil.which("flyctl") or shutil.which("fly")
    if not flyctl:
        messages.append("flyctl not found; static Fly config validation passed")
        return messages
    if not os.environ.get("FLY_API_TOKEN"):
        messages.append(
            "flyctl found but FLY_API_TOKEN is not set; skipped credentialed fly config validate"
        )
        return messages

    with tempfile.TemporaryDirectory() as tmp:
        tmpdir = pathlib.Path(tmp)
        runtime_tmp = tmpdir / "fly.toml"
        dashboard_tmp = tmpdir / "fly.dashboard.toml"
        runtime_tmp.write_text(
            runtime_path.read_text()
            .replace("<app-name>", "af-stack-ci-runtime")
            .replace("<primary-region>", "iad")
        )
        dashboard_tmp.write_text(
            dashboard_path.read_text()
            .replace("<dashboard-app-name>", "af-stack-ci-dashboard")
            .replace("<runtime-app-name>", "af-stack-ci-runtime")
            .replace("<primary-region>", "iad")
        )
        for config in (runtime_tmp, dashboard_tmp):
            result = run([flyctl, "config", "validate", "--config", str(config)])
            if result.returncode != 0:
                raise CheckError(
                    f"fly config validate failed for {config.name}:\n{result.stdout}\n{result.stderr}"
                )
    messages.append("flyctl config validate passed")
    return messages


def check_railway() -> list[str]:
    path = require_file("deploy/railway/railway.json")
    data = json.loads(path.read_text())
    services = data.get("services", [])
    require(isinstance(services, list) and services, "Railway template must define services")
    postgres = find_service(services, "postgres")
    agentfield = find_service(services, "agentfield")
    litellm = find_service(services, "litellm")
    runtime = find_service(services, "runtime")
    dashboard = find_service(services, "dashboard")
    customer = find_service(services, "customer")

    require(
        postgres.get("source", {}).get("image") == "pgvector/pgvector:pg16",
        "Railway Postgres must use pgvector PG16",
    )
    require(
        agentfield.get("source", {}).get("image") == "agentfield/control-plane:latest",
        "Railway AgentField image mismatch",
    )
    require(
        "AGENTFIELD_STORAGE_POSTGRES_URL" in agentfield.get("variables", {}),
        "Railway AgentField must receive database URL",
    )
    require(
        litellm.get("build", {}).get("dockerfilePath") == "deploy/railway/litellm.Dockerfile",
        "Railway LiteLLM Dockerfile mismatch",
    )
    require(
        litellm.get("deploy", {}).get("healthcheckPath") == "/health/readiness",
        "Railway LiteLLM healthcheck must be /health/readiness",
    )
    require(
        runtime.get("build", {}).get("dockerfilePath") == "services/runtime/Dockerfile",
        "Railway runtime Dockerfile mismatch",
    )
    require(
        runtime.get("deploy", {}).get("healthcheckPath") == "/health",
        "Railway runtime healthcheck must be /health",
    )
    require(
        dashboard.get("build", {}).get("dockerfilePath") == "apps/dashboard/Dockerfile",
        "Railway dashboard Dockerfile mismatch",
    )
    require(
        dashboard.get("deploy", {}).get("healthcheckPath") == "/",
        "Railway dashboard healthcheck must be /",
    )
    require(
        customer.get("build", {}).get("dockerfilePath") == "apps/customer-app/Dockerfile",
        "Railway customer app Dockerfile mismatch",
    )
    require(
        customer.get("deploy", {}).get("healthcheckPath") == "/",
        "Railway customer app healthcheck must be /",
    )
    require(
        "AF_STACK_DATABASE_URL" in runtime.get("variables", {}),
        "Railway runtime must receive database URL",
    )
    require(
        "AF_STACK_AGENTFIELD_URL" in runtime.get("variables", {}),
        "Railway runtime must receive AgentField URL",
    )
    require(
        "AF_STACK_LITELLM_URL" in runtime.get("variables", {}),
        "Railway runtime must receive LiteLLM URL",
    )
    require(
        "AF_STACK_DEMO_MODE" in runtime.get("variables", {}), "Railway runtime must set demo mode"
    )
    require(
        "DATABASE_URL" in dashboard.get("variables", {}),
        "Railway dashboard must receive database URL",
    )
    require(
        "DATABASE_URL" in customer.get("variables", {}),
        "Railway customer app must receive database URL",
    )
    require(
        "RUNTIME_URL" in customer.get("variables", {}),
        "Railway customer app must receive runtime URL",
    )
    return ["Railway JSON parsed and required BackAI services validated"]


def check_render() -> list[str]:
    path = require_file("deploy/render/render.yaml")
    text = path.read_text()
    ruby = shutil.which("ruby")
    if ruby:
        parsed = run([ruby, "-e", "require 'yaml'; YAML.load_file(ARGV[0]); puts 'ok'", str(path)])
        if parsed.returncode != 0:
            raise CheckError(f"Render YAML parse failed:\n{parsed.stdout}\n{parsed.stderr}")

    for pattern, message in (
        (
            r"databases:\s*\n\s*-\s+name:\s+af-stack-postgres",
            "Render must define af-stack-postgres database",
        ),
        (r"name:\s+af-stack-runtime", "Render must define runtime service"),
        (r"dockerfilePath:\s+\./services/runtime/Dockerfile", "Render runtime Dockerfile mismatch"),
        (r"healthCheckPath:\s+/health", "Render runtime health check must be /health"),
        (r"name:\s+af-stack-dashboard", "Render must define dashboard service"),
        (r"dockerfilePath:\s+\./apps/dashboard/Dockerfile", "Render dashboard Dockerfile mismatch"),
        (r"healthCheckPath:\s+/", "Render dashboard health check must be /"),
        (
            r"fromDatabase:\s*\n\s*name:\s+af-stack-postgres",
            "Render services must use managed Postgres",
        ),
    ):
        require(re.search(pattern, text), message)
    return [
        "Render Blueprint parsed/contract-checked" if ruby else "Render Blueprint contract-checked"
    ]


def check_prod_compose() -> list[str]:
    path = require_file("docker-compose.prod.yml")
    text = path.read_text()
    caddyfile = require_file("deploy/caddy/Caddyfile").read_text()
    for needle in (
        "caddy:",
        "agentfield:",
        "runtime:",
        "dashboard:",
        "AF_STACK_DATABASE_URL",
        "AF_STACK_S3_ADAPTER: s3",
    ):
        require(needle in text, f"prod compose missing {needle!r}")
    require(
        "/health" in caddyfile and "/ready" in caddyfile,
        "prod Caddyfile must expose runtime health endpoints",
    )
    require(
        not re.search(r"^\s*-\s*/var/run/docker\.sock\b", text, re.MULTILINE),
        "prod compose must not mount docker.sock",
    )

    docker = shutil.which("docker")
    if not docker:
        return ["docker not found; static prod compose validation passed"]

    result = run([docker, "compose", "-f", str(path), "config", "--quiet"])
    if result.returncode != 0:
        raise CheckError(f"prod compose config failed:\n{result.stdout}\n{result.stderr}")
    return ["prod compose config passed"]


def main() -> int:
    checks = [
        ("Helm", check_helm),
        ("Fly.io", check_fly),
        ("Railway", check_railway),
        ("Render", check_render),
        ("Production compose", check_prod_compose),
    ]
    failed = False
    for name, check in checks:
        print(f"==> {name}")
        try:
            for message in check():
                print(f"  ok: {message}")
        except Exception as exc:
            failed = True
            print(f"  FAIL: {exc}", file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    os.chdir(ROOT)
    raise SystemExit(main())
