// Dense sequential original-row projection. No per-target SeekToRow.
package main
import(
 "crypto/sha256";"encoding/hex";"encoding/json";"flag";"fmt";"io";"os";"path/filepath";"regexp";"sort";"strconv";"sync";"time"
 "github.com/parquet-go/parquet-go";"github.com/parquet-go/parquet-go/compress/zstd"
 "openalexstats/research/country-enrichment-v3/metadata/core"
)
type Target struct{ID uint64 `parquet:"id"`;Ordinal uint64 `parquet:"ordinal"`;Year int32 `parquet:"year"`;Type string `parquet:"type"`;XPAC bool `parquet:"is_xpac"`;Index int32 `parquet:"source_index"`;Row uint64 `parquet:"raw_row_index"`}
type oa struct{OA *bool `parquet:"is_oa"`}
type Raw struct{ID *string `parquet:"id"`;Year *int32 `parquet:"publication_year"`;Type *string `parquet:"type"`;XPAC *bool `parquet:"is_xpac"`;DOI *string `parquet:"doi"`;IDs map[string]*string `parquet:"ids"`;Title *string `parquet:"display_name"`;OA *oa `parquet:"open_access"`}
type Row struct{ID uint64 `parquet:"id"`;Ordinal uint64 `parquet:"ordinal"`;Year int32 `parquet:"year"`;Type string `parquet:"type"`;XPAC bool `parquet:"is_xpac"`;Index int32 `parquet:"source_index"`;RawRow uint64 `parquet:"raw_row_index"`;RawDOI string `parquet:"raw_doi"`;RawPMID string `parquet:"raw_pmid"`;Title string `parquet:"title"`;OA bool `parquet:"is_oa"`;Pass bool `parquet:"raw_identity_pass"`;DOI string `parquet:"doi"`;PMID string `parquet:"pmid"`}
type Part struct{Index int `json:"source_index"`;RawPath string `json:"raw_path"`;CachePath string `json:"cache_path"`;Path string `json:"path"`;SHA string `json:"sha256"`;Rows uint64 `json:"rows"`;Core uint64 `json:"core"`;XPAC uint64 `json:"xpac"`}
type Receipt struct{Index int `json:"source_index"`;Path string `json:"path"`;Rows uint64 `json:"rows"`;Core uint64 `json:"core"`;XPAC uint64 `json:"xpac"`;Bytes int64 `json:"bytes"`;SHA string `json:"sha256"`;ManifestSHA string `json:"input_manifest_sha256"`;InputSHA string `json:"input_part_sha256"`;RawPath string `json:"raw_path"`;CachePath string `json:"cache_path"`;Seconds float64 `json:"elapsed_seconds"`;Failures int `json:"raw_identity_failures"`;Engine string `json:"engine"`}
func val(p *string)string{if p==nil{return ""};return *p};func truth(p *bool)bool{return p!=nil&&*p};func must(e error){if e!=nil{panic(e)}}
func hash(p string)string{f,e:=os.Open(p);must(e);defer f.Close();h:=sha256.New();_,e=io.Copy(h,f);must(e);return hex.EncodeToString(h.Sum(nil))}
func save(p string,v any){b,e:=json.MarshalIndent(v,"","  ");must(e);must(os.WriteFile(p+".partial",append(b,'\n'),0600));must(os.Rename(p+".partial",p))}
var bare=regexp.MustCompile(`^[1-9][0-9]*$`);var canonical=regexp.MustCompile(`^https?://pubmed\.ncbi\.nlm\.nih\.gov/([1-9][0-9]*)/?$`)
func pmid(s string)string{if bare.MatchString(s){return s};if m:=canonical.FindStringSubmatch(s);len(m)==2{return m[1]};return ""}
func run(part Part,out,manifestSHA string)Receipt{
 p:=filepath.Join(out,fmt.Sprintf("part-%05d.parquet",part.Index));meta:=filepath.Join(out,fmt.Sprintf("part-%05d.json",part.Index));var old Receipt
 if b,e:=os.ReadFile(meta);e==nil&&json.Unmarshal(b,&old)==nil&&old.ManifestSHA==manifestSHA&&old.Rows==part.Rows&&old.SHA==hash(p){return old}
 if hash(part.Path)!=part.SHA{panic("thin checksum changed")};f,e:=os.Open(part.Path);must(e);r:=parquet.NewGenericReader[Target](f);ts:=[]Target{};buf:=make([]Target,4096)
 for{n,e:=r.Read(buf);ts=append(ts,buf[:n]...);if e==io.EOF{break};must(e)};r.Close();f.Close();if uint64(len(ts))!=part.Rows{panic("thin count mismatch")};if !sort.SliceIsSorted(ts,func(i,j int)bool{return ts[i].Row<ts[j].Row}){sort.Slice(ts,func(i,j int)bool{return ts[i].Row<ts[j].Row})}
 started:=time.Now();source,e:=os.Open(part.RawPath);must(e);defer source.Close();st,e:=source.Stat();must(e);pf,e:=parquet.OpenFile(source,st.Size(),parquet.SkipPageIndex(true),parquet.SkipBloomFilters(true));must(e);reader:=parquet.NewGenericReader[Raw](pf);defer reader.Close()
 dest,e:=os.OpenFile(p+".partial",os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0600);must(e);w:=parquet.NewGenericWriter[Row](dest,parquet.Compression(&zstd.Codec{Level:zstd.SpeedFastest,Concurrency:1}),parquet.MaxRowsPerRowGroup(100000));rawbuf:=make([]Raw,2048);rows:=make([]Row,0,2048);var pos uint64;target:=0;rec:=Receipt{part.Index,p,0,0,0,0,"",manifestSHA,part.SHA,part.RawPath,part.CachePath,0,0,"go-sequential-batch-v1"}
 flush:=func(){if len(rows)>0{_,e:=w.Write(rows);must(e);rows=rows[:0]}}
 for target<len(ts){for i:=range rawbuf{rawbuf[i]=Raw{}};n,e:=reader.Read(rawbuf);if e!=nil&&e!=io.EOF{panic(e)};if n==0{panic("source exhausted before last UNKNOWN target")}
  for _,raw:=range rawbuf[:n]{if target>=len(ts){break};t:=ts[target];if pos==t.Row{year:=int32(0);if raw.Year!=nil{year=*raw.Year};if val(raw.ID)!="https://openalex.org/W"+strconv.FormatUint(t.ID,10)||year!=t.Year||val(raw.Type)!=t.Type||truth(raw.XPAC)!=t.XPAC{panic(fmt.Sprintf("raw identity mismatch part=%d row=%d",part.Index,t.Row))};d:=val(raw.DOI);if raw.DOI==nil{d=val(raw.IDs["doi"])};pm:=val(raw.IDs["pmid"]);isOA:=false;if raw.OA!=nil{isOA=truth(raw.OA.OA)};rows=append(rows,Row{t.ID,t.Ordinal,t.Year,t.Type,t.XPAC,t.Index,t.Row,d,pm,val(raw.Title),isOA,true,core.DOI(d),pmid(pm)});rec.Rows++;if t.XPAC{rec.XPAC++}else{rec.Core++};target++;if len(rows)==cap(rows){flush()}};pos++}
 }
 flush();must(w.Close());must(dest.Close());must(os.Rename(p+".partial",p));if rec.Rows!=part.Rows||rec.Core!=part.Core||rec.XPAC!=part.XPAC{panic("output count mismatch")};rec.SHA=hash(p);st,e=os.Stat(p);must(e);rec.Bytes=st.Size();rec.Seconds=time.Since(started).Seconds();save(meta,rec);return rec
}
func main(){manifest:=flag.String("manifest","","root UNKNOWN manifest");out:=flag.String("out","","shared index output");one:=flag.Int("part",-1,"single source part benchmark");workers:=flag.Int("workers",4,"HDD stream concurrency");flag.Parse();must(os.MkdirAll(*out,0755));b,e:=os.ReadFile(*manifest);must(e);var m struct{Parts []Part `json:"parts"`};must(json.Unmarshal(b,&m));manifestSHA:=hash(*manifest);started:=time.Now()
 if *one>=0{r:=run(m.Parts[*one],*out,manifestSHA);fmt.Printf("%s\n",mustJSON(r));return}
 jobs:=make(chan Part);done:=make(chan Receipt,*workers);var wg sync.WaitGroup;for i:=0;i<*workers;i++{wg.Add(1);go func(){defer wg.Done();for p:=range jobs{done<-run(p,*out,manifestSHA)}}()};go func(){for _,p:=range m.Parts{jobs<-p};close(jobs);wg.Wait();close(done)}()
 results:=[]Receipt{};var rows,co,xp uint64;for r:=range done{results=append(results,r);rows+=r.Rows;co+=r.Core;xp+=r.XPAC;if len(results)%50==0{fmt.Printf("parts=%d/2446 rows=%d seconds=%.1f\n",len(results),rows,time.Since(started).Seconds())}}
 if rows!=277418468||co!=150377414||xp!=127041054||len(results)!=2446{panic("global frame does not close")};sort.Slice(results,func(i,j int)bool{return results[i].Index<results[j].Index});save(filepath.Join(*out,"manifest.json"),map[string]any{"status":"complete_shared_original_identifier_projection","parts":results,"rows":rows,"core":co,"xpac":xp,"input_manifest_sha256":manifestSHA,"source_mutated":false,"metadata_api_calls":0,"elapsed_seconds":time.Since(started).Seconds(),"workers":*workers,"engine":"go-sequential-batch-v1","schema":"id,ordinal,year,type,is_xpac,source_index,raw_row_index,raw_doi,raw_pmid,title,is_oa,raw_identity_pass,doi,pmid"});fmt.Printf("COMPLETE rows=%d seconds=%.1f\n",rows,time.Since(started).Seconds())
}
func mustJSON(v any)string{b,e:=json.Marshal(v);must(e);return string(b)}
