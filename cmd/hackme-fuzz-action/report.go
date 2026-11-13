package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"
)

// commentMarker lets the action find and update its own sticky PR comment.
const commentMarker = "<!-- hackme-fuzz-action -->"

const maxCodeSpanRunes = 300

// mdCode renders untrusted text (paths, gate errors) as an inline code span
// that cannot break out: backticks, control and bidi/format chars are replaced.
func mdCode(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= maxCodeSpanRunes {
			b.WriteRune('…')
			break
		}
		switch {
		case r == '`':
			r = '\''
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			r = ' '
		}
		b.WriteRune(r)
		n++
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		out = "-"
	}
	return "`" + out + "`"
}

// mdCell is mdCode for a table cell: GFM splits cells on '|' even inside code,
// and "\|" escaping gets ambiguous next to user backslashes, so swap the char.
func mdCell(s string) string {
	return mdCode(strings.ReplaceAll(s, "|", "¦"))
}

// payloadValue keeps the table readable: seed corpora are shown as a count.
func payloadValue(k string, v any) string {
	if strings.HasPrefix(k, "seed_") {
		if s, ok := v.([]any); ok {
			return fmt.Sprintf("[%d items]", len(s))
		}
	}
	return fmt.Sprint(v)
}

// RenderMarkdown is the job summary / PR comment body.
func RenderMarkdown(p *Plan) string {
	var b strings.Builder
	b.WriteString(commentMarker + "\n")
	if p.OK() {
		b.WriteString("### ✅ HackMe Dig — dry-run: guard wasm passes the pool gate\n\n")
	} else {
		b.WriteString("### ❌ HackMe Dig — dry-run: guard wasm rejected by the pool gate\n\n")
	}
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Source | %s |\n", mdCell(p.Source))
	if p.DigPreset != "" {
		fmt.Fprintf(&b, "| Dig preset | %s |\n", mdCell(p.DigPreset))
	}
	if p.GuardPack != "" {
		fmt.Fprintf(&b, "| Guard pack | %s |\n", mdCell(p.GuardPack))
	}
	fmt.Fprintf(&b, "| Package | %s |\n", mdCell(p.DigPackage))
	fmt.Fprintf(&b, "| Guard | %s · %d bytes wasm |\n", mdCell(p.Wasm.Path), p.Wasm.Bytes)
	fmt.Fprintf(&b, "| sha256 | %s |\n", mdCell(p.Wasm.SHA256))
	if v, ok := p.Payload["budget_runs"]; ok {
		fmt.Fprintf(&b, "| Budget | %v runs · %vs · %v HMC |\n", v, p.Payload["budget_seconds"], p.Payload["budget_hmc"])
	}
	if !p.OK() {
		fmt.Fprintf(&b, "\n**Gate error:** %s\n", mdCode(p.Wasm.Error))
	}
	fmt.Fprintf(&b, "\nNothing was sent: this is a dry-run of %s.\n", mdCode(p.Endpoint))

	b.WriteString("\n<details><summary>Payload that would be sent</summary>\n\n")
	keys := make([]string, 0, len(p.Payload))
	for k := range p.Payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("| key | value |\n|---|---|\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "| %s | %s |\n", mdCell(k), mdCell(payloadValue(k, p.Payload[k])))
	}
	b.WriteString("\n</details>\n")
	return b.String()
}

// Outputs are the step outputs exposed to later workflow steps.
func Outputs(p *Plan) [][2]string {
	return [][2]string{
		{"valid", fmt.Sprint(p.OK())},
		{"source", p.Source},
		{"dig-preset", p.DigPreset},
		{"guard-pack", p.GuardPack},
		{"dig-package", p.DigPackage},
		{"wasm-sha256", p.Wasm.SHA256},
		{"wasm-bytes", fmt.Sprint(p.Wasm.Bytes)},
	}
}

// writeOutputs appends key=value lines in $GITHUB_OUTPUT format. Every value
// here is an allowlisted id, hex digest or number; anything with a line break
// is refused rather than escaped, so a value can never forge another output.
func writeOutputs(w io.Writer, kv [][2]string) error {
	for _, e := range kv {
		if strings.ContainsAny(e[0], "=\r\n") || strings.ContainsAny(e[1], "\r\n") {
			return fmt.Errorf("output %q: refusing value with line break", e[0])
		}
		if _, err := fmt.Fprintf(w, "%s=%s\n", e[0], e[1]); err != nil {
			return err
		}
	}
	return nil
}

func appendFile(path string, write func(io.Writer) error) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	// Close errors matter on a write handle (data may not have landed), so
	// they're reported even when the write itself already failed.
	return errors.Join(write(f), f.Close())
}
