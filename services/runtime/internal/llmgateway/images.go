// SPDX-License-Identifier: Apache-2.0

package llmgateway

// ImagesRequest is the inbound /images/generations body shape.
//
// Matches OpenAI's `openai.images.generate()` request.
type ImagesRequest struct {
	Model          string `json:"model,omitempty"`
	Prompt         string `json:"prompt"`
	N              *int   `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	Style          string `json:"style,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	User           string `json:"user,omitempty"`
}

// ImagesResponse is the /images/generations response.
type ImagesResponse struct {
	Created int64       `json:"created"`
	Data    []ImageItem `json:"data"`
}

// ImageItem is one image in data[].
//
// Either URL or B64JSON is populated depending on `response_format`.
type ImageItem struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}
