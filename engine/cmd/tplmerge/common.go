package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// decodeStrict reads one JSON value and refuses a field the type does not have, the
// same check author.Source itself is read with
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("has more than one value")
	}
	return nil
}

// prepareOutDir makes sure out exists and is safe to write into, refusing a folder
// that already holds files unless force is set
func prepareOutDir(out string, force bool) error {
	entries, err := os.ReadDir(out)
	switch {
	case os.IsNotExist(err):
		return os.MkdirAll(out, 0o755)
	case err != nil:
		return fmt.Errorf("reading -out %q: %w", out, err)
	case len(entries) > 0 && !force:
		return fmt.Errorf("-out %q already has files in it (pass -force to write into it anyway)", out)
	default:
		return nil
	}
}

// copyFile copies src to dst, making dst's parent directories as needed
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyDir copies every file under src to the same relative path under dst
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
}

// sha256File is the lowercase hex sha256 of a file's content
func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
