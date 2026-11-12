// Prepare source-only, strict-personal Crossref assertion pool; no Work patch.
package main
import(
 "bufio";"bytes";"compress/gzip";"crypto/sha256";"encoding/hex";"encoding/json";"flag";"fmt";"io";"os";"path/filepath";"strings";"sync";"time"
 "github.com/parquet-go/parquet-go";"github.com/parquet-go/parquet-go/compress/zstd"
 "openalexstats/research/country-enrichment-v3/metadata/core"
)
type assertion struct{DOI string `json:"DOI"`;Relationship string `json:"relationship"`;Role string `json:"contributor-role"`;Seq int `json:"seq_num"`;Contributor struct{Given,Family,Suffix string;ORCID string `json:"ORCID"`} `json:"contributor"`;Organisation string `json:"organisation-string"`;ROR string `json:"ror-id"`;AssertedBy string `json:"ror-id-asserted-by"`}
type row struct{DOI string `parquet:"doi"`;Given string `parquet:"given"`;Family string `parquet:"family"`;ORCID string `parquet:"orcid"`;Organisation string `parquet:"organisation"`;Countries []string `parquet:"countries,list"`;SourceLine uint64 `parquet:"source_line"`;LineSHA string `parquet:"source_line_sha256"`;Seq int32 `parquet:"seq_num"`;Payload string `parquet:"private_assertion_json"`}
type line struct{N uint64;B []byte}
type summary struct{Worker int;Parsed,Kept uint64;Path,SHA string;Bytes int64}
func must(e error){if e!=nil{panic(e)}}
func hash(p string)string{f,e:=os.Open(p);must(e);defer f.Close();h:=sha256.New();_,e=io.Copy(h,f);must(e);return hex.EncodeToString(h.Sum(nil))}
func main(){
 path:=flag.String("dump","","official frozen gzip");out:=flag.String("out","","private source pool");alias:=flag.String("rules","","frozen aliases");workers:=flag.Int("workers",24,"decode concurrency");flag.Parse();must(os.MkdirAll(*out,0700));must(os.Chmod(*out,0700));rules,e:=core.LoadRules(*alias);must(e)
 f,e:=os.Open(*path);must(e);defer f.Close();hs:=sha256.New();gz,e:=gzip.NewReader(io.TeeReader(f,hs));must(e);defer gz.Close();reader:=bufio.NewReaderSize(gz,8<<20)
 jobs:=make(chan line,1024);results:=make(chan summary,*workers);var wg sync.WaitGroup
 for i:=0;i<*workers;i++{wg.Add(1);go func(worker int){defer wg.Done();p:=filepath.Join(*out,fmt.Sprintf("part-%02d.parquet",worker));file,e:=os.OpenFile(p+".partial",os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0600);must(e);w:=parquet.NewGenericWriter[row](file,parquet.Compression(&zstd.Codec{Level:zstd.SpeedFastest,Concurrency:1}),parquet.MaxRowsPerRowGroup(100000));buf:=make([]row,0,1024);s:=summary{Worker:worker,Path:p}
  flush:=func(){if len(buf)>0{_,e:=w.Write(buf);must(e);buf=buf[:0]}}
  for l:=range jobs{s.Parsed++;var a assertion;must(json.Unmarshal(l.B,&a));if a.Relationship!="contributor-affiliation"||a.Role!="author"||strings.TrimSpace(a.Contributor.Given)==""||strings.TrimSpace(a.Contributor.Family)==""{continue};orcid:=core.ORCID(a.Contributor.ORCID);if orcid==""{continue};countries,amb:=rules.Countries(a.Organisation);if amb||len(countries)==0{continue};d:=core.DOI(a.DOI);if d==""{continue};h:=sha256.Sum256(l.B);payload,e:=json.Marshal(a);must(e);buf=append(buf,row{d,a.Contributor.Given,a.Contributor.Family,orcid,a.Organisation,countries,l.N,hex.EncodeToString(h[:]),int32(a.Seq),string(payload)});s.Kept++;if len(buf)==cap(buf){flush()}}
  flush();must(w.Close());must(file.Close());must(os.Rename(p+".partial",p));s.SHA=hash(p);st,e:=os.Stat(p);must(e);s.Bytes=st.Size();results<-s
 }(i)}
 started:=time.Now();var n,possible uint64
 for{b,e:=reader.ReadBytes('\n');if e!=nil&&e!=io.EOF{panic(e)};if len(bytes.TrimSpace(b))>0{n++;if bytes.Contains(b,[]byte(`"contributor-affiliation"`))&&bytes.Contains(b,[]byte(`"ORCID"`)){possible++;jobs<-line{n,append([]byte(nil),bytes.TrimSpace(b)...)};};if n%10000000==0{fmt.Printf("lines=%d possible_personal_orcid=%d seconds=%.1f\n",n,possible,time.Since(started).Seconds())}};if e==io.EOF{break}}
 close(jobs);wg.Wait();close(results);sourceSHA:=hex.EncodeToString(hs.Sum(nil));if n!=140906929||sourceSHA!="be7c9fb2c5bfdee6eb052eadace6c18efeb860abededc73fde5962ff5ecae992"{panic("source fingerprint mismatch")}
 parts:=[]summary{};var kept uint64;for s:=range results{parts=append(parts,s);kept+=s.Kept};exe,e:=os.Executable();must(e)
 m:=map[string]any{"status":"complete_frozen_crossref_source_pool","source_file":*path,"source_sha256":sourceSHA,"source_lines":n,"possible_personal_orcid_lines":possible,"eligible_source_assertions":kept,"parts":parts,"rules_sha256":hash(*alias),"tool_sha256":hash(exe),"seconds":time.Since(started).Seconds(),"work_country_patch_generated":false,"source_mutated":false};b,e:=json.MarshalIndent(m,"","  ");must(e);must(os.WriteFile(filepath.Join(*out,"manifest.json"),append(b,'\n'),0600));fmt.Printf("DONE source_lines=%d assertions=%d seconds=%.1f\n",n,kept,time.Since(started).Seconds())
}
