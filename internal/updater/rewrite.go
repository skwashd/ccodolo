package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// rewriteFile reads path, applies transform, and writes back if changed.
func rewriteFile(path string, transform func([]byte) ([]byte, error)) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dst, err := transform(src)
	if err != nil {
		return err
	}
	if bytes.Equal(src, dst) {
		return fmt.Errorf("no change: transform produced identical content for %s", path)
	}
	return os.WriteFile(path, dst, 0o644)
}

// RewriteToolDefaultTag rewrites the DefaultTag of tool `name` in tool.go from oldTag to newTag.
// It anchors on the unique Name: "<name>" literal, then replaces the next DefaultTag value.
func RewriteToolDefaultTag(src []byte, name, oldTag, newTag string) ([]byte, error) {
	// Build regex: anchor on Name: "<name>", then (lazily) find DefaultTag: "<oldTag>"
	nameRE := regexp.MustCompile(`Name:\s+"` + regexp.QuoteMeta(name) + `"`)
	nameLoc := nameRE.FindIndex(src)
	if nameLoc == nil {
		return nil, fmt.Errorf("tool %q: Name literal not found in source", name)
	}
	after := src[nameLoc[0]:]
	tagStr := `DefaultTag:\s+"` + regexp.QuoteMeta(oldTag) + `"`
	tagRE := regexp.MustCompile(tagStr)
	tagLoc := tagRE.FindIndex(after)
	if tagLoc == nil {
		return nil, fmt.Errorf("tool %q: DefaultTag %q not found in block starting at offset %d", name, oldTag, nameLoc[0])
	}
	absStart := nameLoc[0] + tagLoc[0]
	absEnd := nameLoc[0] + tagLoc[1]
	// Preserve the whitespace between DefaultTag: and the value
	matched := string(after[tagLoc[0]:tagLoc[1]])
	newMatched := strings.Replace(matched, `"`+oldTag+`"`, `"`+newTag+`"`, 1)
	result := make([]byte, 0, len(src)+(len(newTag)-len(oldTag)))
	result = append(result, src[:absStart]...)
	result = append(result, []byte(newMatched)...)
	result = append(result, src[absEnd:]...)
	return result, nil
}

// RewriteHelmURL rewrites the hardcoded helm version in the install URL.
// Pattern: .../refs/tags/v<oldVer>/scripts/get-helm-4
func RewriteHelmURL(src []byte, oldVer, newVer string) ([]byte, error) {
	old := []byte("refs/tags/v" + oldVer + "/")
	newB := []byte("refs/tags/v" + newVer + "/")
	if !bytes.Contains(src, old) {
		return nil, fmt.Errorf("helm URL version %q not found in source", oldVer)
	}
	return bytes.Replace(src, old, newB, 1), nil
}

// RewriteDockerfileArg rewrites "ARG <argName>=<oldVal>" in Dockerfile.tmpl.
func RewriteDockerfileArg(src []byte, argName, oldVal, newVal string) ([]byte, error) {
	old := []byte("ARG " + argName + "=" + oldVal)
	newB := []byte("ARG " + argName + "=" + newVal)
	if !bytes.Contains(src, old) {
		return nil, fmt.Errorf("ARG %s=%s not found in Dockerfile.tmpl", argName, oldVal)
	}
	return bytes.Replace(src, old, newB, 1), nil
}

// RewriteTestVersion rewrites all occurrences of oldVer in the test source.
// Used when golang/java/python default version advances, to keep TestDefaultVersion passing.
// This replaces all string occurrences of oldVer (bare digits) in the file.
func RewriteTestVersion(src []byte, oldVer, newVer string) ([]byte, error) {
	if !bytes.Contains(src, []byte(oldVer)) {
		return nil, fmt.Errorf("version %q not found in test file", oldVer)
	}
	return bytes.ReplaceAll(src, []byte(oldVer), []byte(newVer)), nil
}
