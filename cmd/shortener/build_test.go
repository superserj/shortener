package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrintBuildInfoWithValues(t *testing.T) {
	setBuildInfo(t, "v1.0.1", "2026/07/29 23:15:00", "abc1234")

	var out bytes.Buffer
	printBuildInfo(&out)

	assert.Equal(t, "Build version: v1.0.1\nBuild date: 2026/07/29 23:15:00\nBuild commit: abc1234\n", out.String())
}

func TestPrintBuildInfoWithoutValues(t *testing.T) {
	setBuildInfo(t, "", "", "")

	var out bytes.Buffer
	printBuildInfo(&out)

	assert.Equal(t, "Build version: N/A\nBuild date: N/A\nBuild commit: N/A\n", out.String())
}

func TestPrintBuildInfoPartial(t *testing.T) {
	setBuildInfo(t, "v1.0.1", "", "abc1234")

	var out bytes.Buffer
	printBuildInfo(&out)

	assert.Contains(t, out.String(), "Build version: v1.0.1\n")
	assert.Contains(t, out.String(), "Build date: N/A\n")
	assert.Contains(t, out.String(), "Build commit: abc1234\n")
}

// setBuildInfo подменяет сведения о сборке на время теста и возвращает прежние
// значения обратно после его завершения.
func setBuildInfo(t *testing.T, version, date, commit string) {
	t.Helper()

	oldVersion, oldDate, oldCommit := buildVersion, buildDate, buildCommit
	t.Cleanup(func() {
		buildVersion, buildDate, buildCommit = oldVersion, oldDate, oldCommit
	})
	buildVersion, buildDate, buildCommit = version, date, commit
}
