#!/usr/bin/env python3
"""
Hit the AF Stack LLM gateway using the official OpenAI Python SDK.

Setup:
    pip install openai

Usage:
    python scripts/openai-demo.py "What is AF Stack?"

Multi-tenancy is OFF in this example, so the api_key value is ignored.
We pass a placeholder so the SDK doesn't refuse to construct the client.
"""

import os
import sys

try:
    from openai import OpenAI
except ImportError:
    print("error: openai package not installed. run: pip install openai", file=sys.stderr)
    sys.exit(1)


def main() -> int:
    prompt = " ".join(sys.argv[1:]) or "Reply in one short sentence: what is an LLM gateway?"

    base_url = os.environ.get("AF_STACK_LLM_URL", "http://localhost:8080/api/v1/llm")
    model = os.environ.get("AF_STACK_MODEL", "qwen/qwen-2.5-72b-instruct")

    client = OpenAI(
        base_url=base_url,
        # MT is off so this is a placeholder. With MT on, pass a real
        # tenant key issued via the dashboard (Customers -> API Keys).
        api_key="not-needed-mt-off",
    )

    resp = client.chat.completions.create(
        model=model,
        messages=[
            {"role": "system", "content": "You are a terse assistant. Reply in one short sentence."},
            {"role": "user", "content": prompt},
        ],
        max_tokens=80,
    )

    msg = resp.choices[0].message.content or ""
    print(msg.strip())
    print()
    print(
        f"  model={resp.model}",
        f"prompt_tokens={resp.usage.prompt_tokens}",
        f"completion_tokens={resp.usage.completion_tokens}",
        sep="  ",
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
