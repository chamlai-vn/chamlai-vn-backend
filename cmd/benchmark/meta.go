package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const metaFileName = "meta.json"

// readMeta loads runDir/meta.json, or a zero RunMeta if it doesn't exist yet
// (the first phase to run, normally -gen, creates it).
func readMeta(runDir string) RunMeta {
	raw, err := os.ReadFile(filepath.Join(runDir, metaFileName))
	if err != nil {
		return RunMeta{}
	}
	var m RunMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return RunMeta{}
	}
	return m
}

// writeMeta persists m to runDir/meta.json, pretty-printed for human review.
// Each phase reads the current meta, fills in its own fields, and writes it
// back — see main.go's package doc for why provenance matters here.
func writeMeta(runDir string, m RunMeta) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("benchmark: marshal meta: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, metaFileName), raw, 0o644); err != nil {
		return fmt.Errorf("benchmark: write meta: %w", err)
	}
	return nil
}

// gitSHA returns the current commit hash, or "" if unavailable (e.g. not a
// git checkout) — provenance is best-effort, never a reason to fail a run.
func gitSHA() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// hashDataset returns a short, stable identifier for the dataset's exact
// contents, so a report can be traced back to precisely which cases produced
// it even if the file is later regenerated with the same case count.
func hashDataset(cases []TestCase) string {
	raw, _ := json.Marshal(cases)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:12]
}

// nowUTC formats the current time consistently for RunMeta.Timestamp.
func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
