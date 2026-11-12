// Receive only a named official PubMed bulk file through SSH stdin.
package main

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func main() {
	if len(os.Args) != 3 || !regexp.MustCompile(`^pubmed26n[0-9]{4}\.xml\.gz$`).MatchString(os.Args[1]) || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(os.Args[2]) {
		panic("Expected fixed PubMed name and MD5")
	}
	dir := "/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/metadata/dumps/pubmed"
	must(os.MkdirAll(dir, 0755))
	p := filepath.Join(dir, os.Args[1])
	t := time.Now()
	f, e := os.Create(p + ".bridge.partial")
	must(e)
	a, b := md5.New(), sha256.New()
	n, e := io.CopyBuffer(io.MultiWriter(f, a, b), os.Stdin, make([]byte, 1<<20))
	must(f.Close())
	must(e)
	m := hex.EncodeToString(a.Sum(nil))
	if m != os.Args[2] {
		panic("Bulk MD5 mismatch")
	}
	must(os.Rename(p+".bridge.partial", p))
	r := map[string]any{"status": "passed", "name": os.Args[1], "bytes": n, "md5": m, "sha256": hex.EncodeToString(b.Sum(nil)), "elapsed_seconds": time.Since(t).Seconds(), "transport": "Windows public-file fetch streamed over SSH stdin; analysis remains229", "per_work_api_calls": 0}
	v, e := json.MarshalIndent(r, "", "  ")
	must(e)
	must(os.WriteFile(p+".bridge-verified.json", append(v, '\n'), 0644))
	fmt.Println(string(v))
}
