package ai

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

//go:embed prompts/*.txt
var prompts embed.FS

type Request struct{ System, Input string }
type LLM interface {
	Generate(context.Context, Request) (string, error)
}

func Email(kind, subject, sender, body string, limited bool) (Request, string, error) {
	if kind != "summary" && kind != "reply" {
		return Request{}, "", errors.New("invalid assistance kind")
	}
	version := "email_" + kind + "_v1"
	prompt, err := prompts.ReadFile("prompts/" + version + ".txt")
	if err != nil {
		return Request{}, "", err
	}
	data, err := json.Marshal(struct {
		Subject    string `json:"subject"`
		Sender     string `json:"sender"`
		Body       string `json:"body"`
		Incomplete bool   `json:"incomplete"`
	}{subject, sender, body, limited})
	return Request{System: string(prompt), Input: string(data)}, version, err
}

func ValidText(text string) bool {
	if len(text) > 8000 || strings.TrimSpace(text) == "" || !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return false
		}
	}
	return true
}

func Business(input string) (Request, string, error) {
	const version = "business_summary_v1"
	prompt, e := prompts.ReadFile("prompts/" + version + ".txt")
	return Request{System: string(prompt), Input: input}, version, e
}

// Question uses the same bounded source projection as summaries. Each question is independent.
func Question(input, question string) (Request, string, error) {
	const version = "business_question_v1"
	prompt, e := prompts.ReadFile("prompts/" + version + ".txt")
	if e != nil {
		return Request{}, version, e
	}
	data, e := json.Marshal(struct {
		Source   json.RawMessage `json:"source"`
		Question string          `json:"question"`
	}{json.RawMessage(input), question})
	if input == "" {
		data = []byte("{}")
		e = nil
	}
	return Request{System: string(prompt), Input: string(data)}, version, e
}

func InvoiceExtraction(input string) (Request, string, error) {
	const version = "invoice_extraction_v1"
	prompt, e := prompts.ReadFile("prompts/" + version + ".txt")
	return Request{System: string(prompt), Input: input}, version, e
}

// ScopedQuestion retains the existing record prompt and versions organization guidance separately.
func ScopedQuestion(kind, input, question string) (Request, string, error) {
	req, version, err := Question(input, question)
	if err != nil || kind != "organization" {
		return req, version, err
	}
	version = "organization_question_v1"
	prompt, err := prompts.ReadFile("prompts/" + version + ".txt")
	req.System = string(prompt)
	return req, version, err
}

// QuestionTurn is saved server-side; clients cannot supply conversation history.
type QuestionTurn struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

func ConversationQuestion(kind, input, question string, history []QuestionTurn) (Request, string, error) {
	if len(history) == 0 {
		return ScopedQuestion(kind, input, question)
	}
	const version = "business_conversation_v1"
	prompt, err := prompts.ReadFile("prompts/" + version + ".txt")
	if err != nil {
		return Request{}, version, err
	}
	if input == "" {
		input = "{}"
	}
	data, err := json.Marshal(struct {
		Kind     string          `json:"kind"`
		Source   json.RawMessage `json:"source"`
		History  []QuestionTurn  `json:"history"`
		Question string          `json:"question"`
	}{kind, json.RawMessage(input), history, question})
	return Request{System: string(prompt), Input: string(data)}, version, err
}
