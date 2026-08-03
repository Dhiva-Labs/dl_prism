// Package router decides which model serves a request: classification,
// policy rules, failover chains, and provider health.
package router

import (
	"regexp"
	"strings"

	"github.com/dhivadhiva/dl_prism/internal/provider"
)

// Profile is what the heuristic classifier concluded about a request.
// No LLM call is involved; classification must stay effectively free.
type Profile struct {
	Task      string `json:"task"` // code | security | docs | vision | general
	Language  string `json:"language,omitempty"`
	Vision    bool   `json:"vision"`
	EstTokens int    `json:"est_tokens"`
}

var (
	fenceRe    = regexp.MustCompile("(?m)^```([A-Za-z0-9+#]+)")
	securityRe = regexp.MustCompile(`(?i)\b(security (audit|review)|vulnerabilit|exploit|cve-\d|pentest|penetration test|xss|sql injection|hardening|threat model)`)
	docsRe     = regexp.MustCompile(`(?i)\b(readme|documentation|docstring|changelog|release notes|api docs)\b`)
	codeRe     = regexp.MustCompile(`(?i)\b(implement|refactor|debug|fix (this|the) (bug|code|test)|write (a|the) (function|class|script|test)|unit test|stack trace|compile error|dockerfile|regex)\b`)
	// Prose mentions like "a python function" or "in rust" name the language
	// without any code fence; adjacency keeps ambiguous names ("go") safe.
	proseLangRe = regexp.MustCompile(`(?i)\b(?:in|using|with)\s+(python|golang|go|rust|javascript|typescript|java|dart|ruby|kotlin|swift|php|sql|bash|c\+\+|c#)\b` +
		`|\b(python|golang|go|rust|javascript|typescript|java|dart|ruby|kotlin|swift|php|sql|bash|c\+\+|c#)\s+(?:function|script|class|code|program|test|snippet|file)\b`)
)

var fenceAlias = map[string]string{
	"py": "python", "python3": "python",
	"js": "javascript", "jsx": "javascript", "node": "javascript",
	"ts": "typescript", "tsx": "typescript",
	"golang": "go",
	"rs":     "rust",
	"rb":     "ruby",
	"kt":     "kotlin",
	"cpp":    "c++", "cxx": "c++", "cc": "c++",
	"cs": "c#", "csharp": "c#",
	"sh": "shell", "bash": "shell", "zsh": "shell",
	"yml": "yaml",
}

// languageHints maps a language to substrings that suggest it. Two or more
// hits identify the language when no code fence names one.
var languageHints = map[string][]string{
	"python":     {"def ", "import ", "self.", "print(", "elif "},
	"go":         {"func ", "package ", ":= ", "chan ", "go func"},
	"rust":       {"fn ", "let mut", "impl ", "::<", "match "},
	"javascript": {"function ", "const ", "=> ", "console.log", "await "},
	"java":       {"public class", "public static", "System.out", "@Override"},
	"c":          {"#include", "int main(", "printf(", "->"},
	"dart":       {"Widget ", "BuildContext", "setState(", "Future<"},
}

func Classify(req *provider.ChatRequest) Profile {
	var b strings.Builder
	for i := range req.Messages {
		b.WriteString(req.Messages[i].Text())
		b.WriteString("\n")
	}
	text := b.String()

	p := Profile{
		Vision:    req.HasImages(),
		EstTokens: len(text) / 4,
		Language:  detectLanguage(text),
	}
	switch {
	case p.Vision:
		p.Task = "vision"
	case securityRe.MatchString(text):
		p.Task = "security"
	case docsRe.MatchString(text):
		p.Task = "docs"
	case p.Language != "" || codeRe.MatchString(text):
		p.Task = "code"
	default:
		p.Task = "general"
	}
	return p
}

func detectLanguage(text string) string {
	for _, m := range fenceRe.FindAllStringSubmatch(text, 4) {
		lang := strings.ToLower(m[1])
		if norm, ok := fenceAlias[lang]; ok {
			return norm
		}
		if _, known := languageHints[lang]; known {
			return lang
		}
		switch lang {
		case "typescript", "ruby", "kotlin", "swift", "c++", "c#", "php", "shell", "sql", "html", "css":
			return lang
		}
		// Unknown fence tag (e.g. "text", "output"): keep scanning.
	}
	if m := proseLangRe.FindStringSubmatch(text); m != nil {
		lang := strings.ToLower(m[1])
		if lang == "" {
			lang = strings.ToLower(m[2])
		}
		if norm, ok := fenceAlias[lang]; ok {
			return norm
		}
		return lang
	}
	best, bestScore := "", 1 // require at least 2 hints
	for lang, hints := range languageHints {
		score := 0
		for _, h := range hints {
			if strings.Contains(text, h) {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = lang, score
		}
	}
	return best
}
