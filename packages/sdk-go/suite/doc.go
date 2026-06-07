// SPDX-License-Identifier: Apache-2.0

// Package suite is the AF Stack Suite SDK for Go.
//
// The suite SDK exposes the operational verbs an app uses daily:
//
//	import "github.com/Agent-Field/backai/packages/sdk-go/suite"
//
//	// Call an agent from app code
//	result, err := suite.Agents.Call(ctx, "notable-ai.summarize", input)
//
// Inside an AgentField agent process, use the AgentField SDK to *define*
// agents. Use this suite SDK to *call* them and to use suite infrastructure.
//
// See https://github.com/Agent-Field/backai for full docs.
package suite

// Version is the SDK version.
const Version = "0.0.1"