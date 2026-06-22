package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	app "claude-tg-agent/internal/app"
)

func main() {
	f, _ := os.Open("/tmp/samples.txt")
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		idx := strings.Index(line, "|")
		if idx < 0 {
			continue
		}
		id, prompt := line[:idx], line[idx+1:]
		r := app.ClassifyComplexity(prompt)
		fmt.Printf("#%s  %-8s  [%s]  %s\n", id, r.Complexity, r.MatchedRule, truncate(prompt, 80))
	}
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
