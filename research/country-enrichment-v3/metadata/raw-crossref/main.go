// Independent byte-level assertion extraction for externally selected audits.
// This program does not interpret affiliations, identities or country labels.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

type Request struct {
	Line uint64 `json:"line"`
	SHA string `json:"source_line_sha256"`
}
func must(e error) { if e != nil { panic(e) } }
func digest(b []byte) string { s:=sha256.Sum256(b);return hex.EncodeToString(s[:]) }

func main() {
	if len(os.Args)!=4 { panic("usage: raw-crossref frozen-dump.gz requests.json out-dir") }
	var requests struct { Lines []Request `json:"lines"` }
	b,e:=os.ReadFile(os.Args[2]);must(e);must(json.Unmarshal(b,&requests))
	need:=map[uint64]string{}
	for _,r:=range requests.Lines {if r.Line==0 || len(r.SHA)!=64 {panic("invalid line request")};if old,ok:=need[r.Line];ok&&old!=r.SHA {panic("conflicting request")};need[r.Line]=r.SHA}
	if len(need)==0 {panic("no externally selected lines")}
	out:=os.Args[3];must(os.MkdirAll(filepath.Join(out,"private"),0700));must(os.MkdirAll(filepath.Join(out,"redacted"),0755))
	f,e:=os.Open(os.Args[1]);must(e);defer f.Close();sourceHash:=sha256.New();g,e:=gzip.NewReader(io.TeeReader(f,sourceHash));must(e);defer g.Close()
	reader:=bufio.NewReaderSize(g,8<<20);email:=regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	var line uint64;rows:=[]map[string]any{};start:=time.Now()
	for {
		raw,e:=reader.ReadBytes('\n');if e!=nil&&e!=io.EOF {panic(e)};trim:=bytes.TrimSpace(raw)
		if len(trim)>0 {
			line++
			if want,ok:=need[line];ok {
				if digest(trim)!=want {panic(fmt.Sprintf("source line fingerprint mismatch at %d",line))}
				if !json.Valid(trim) {panic("source line is not valid JSON")}
				private:=filepath.Join(out,"private",fmt.Sprintf("line-%09d.json",line));redacted:=filepath.Join(out,"redacted",fmt.Sprintf("line-%09d.json",line));clean:=email.ReplaceAll(raw,[]byte("[email removed]"))
				must(os.WriteFile(private,raw,0600));must(os.WriteFile(redacted,clean,0644))
				rows=append(rows,map[string]any{"source_line":line,"source_line_sha256":want,"raw_line_sha256":digest(raw),"private_raw_path":private,"redacted_path":redacted,"redacted_sha256":digest(clean)})
			}
		}
		if e==io.EOF {break}
	}
	sourceSHA:=hex.EncodeToString(sourceHash.Sum(nil));if line!=140906929||sourceSHA!="be7c9fb2c5bfdee6eb052eadace6c18efeb860abededc73fde5962ff5ecae992" {panic("frozen full-source fingerprint mismatch")}
	if len(rows)!=len(need) {panic("missing selected source lines")}
	sort.Slice(rows,func(i,j int)bool{return rows[i]["source_line"].(uint64)<rows[j]["source_line"].(uint64)})
	result:=map[string]any{"status":"source_bytes_extracted_without_interpretation","source_file":os.Args[1],"source_sha256":sourceSHA,"source_nonempty_lines":line,"request_sha256":digest(b),"records":rows,"seconds":time.Since(start).Seconds(),"private_raw_not_for_publication":true,"redactions":[]string{"email substrings"},"identity_or_country_gold_created":false}
	b,e=json.MarshalIndent(result,"","  ");must(e);must(os.WriteFile(filepath.Join(out,"manifest.json"),append(b,'\n'),0644));fmt.Printf("raw lines=%d seconds=%.2f\n",len(rows),time.Since(start).Seconds())
}
