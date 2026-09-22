// mock.go — 测试用 Provider 替身（design.md §12.3）。
// 放在非测试文件中以供 pipeline 等包的测试复用。
package llm

import (
	"context"
	"fmt"
	"strings"
)

// MockResponse 按 prompt 内容片段匹配返回预设响应。
type MockResponse struct {
	MatchPromptContains string
	Content             string
	Err                 error
}

// MockProvider 按 prompt 内容匹配响应的测试替身。
type MockProvider struct {
	responses []MockResponse
	calls     int
}

func NewMockProvider(responses ...MockResponse) *MockProvider {
	return &MockProvider{responses: responses}
}

func (m *MockProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	m.calls++
	content := req.Messages[len(req.Messages)-1].Content
	for _, r := range m.responses {
		if r.MatchPromptContains == "" || strings.Contains(content, r.MatchPromptContains) {
			if r.Err != nil {
				return nil, r.Err
			}
			return &CompletionResponse{Content: r.Content}, nil
		}
	}
	return nil, fmt.Errorf("no mock response")
}

// MultiMockProvider 按调用顺序返回响应（检测轮 → 改写轮的序列场景）。
type MultiMockProvider struct {
	responses []MockResponse
	idx       int
	calls     int
}

func NewMultiMockProvider(responses ...MockResponse) *MultiMockProvider {
	return &MultiMockProvider{responses: responses}
}

func (m *MultiMockProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	m.calls++
	if m.idx >= len(m.responses) {
		return nil, fmt.Errorf("no more mock responses (call %d)", m.calls)
	}
	r := m.responses[m.idx]
	m.idx++
	if r.Err != nil {
		return nil, r.Err
	}
	return &CompletionResponse{Content: r.Content}, nil
}
