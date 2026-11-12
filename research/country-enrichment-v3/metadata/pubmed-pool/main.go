// Full frozen PubMed baseline preparation; does not apply Work countries.
package main
import (
 "bufio";"compress/gzip";"crypto/sha256";"encoding/hex";"encoding/json";"encoding/xml";"flag";"fmt";"io";"os";"path/filepath";"sort";"sync";"time"
 "github.com/parquet-go/parquet-go"
 "github.com/parquet-go/parquet-go/compress/zstd"
)
type PoolRow struct {
 PMID string `parquet:"pmid"`; DOI string `parquet:"doi"`;PMCID string `parquet:"pmcid"`;Title string `parquet:"title"`;Vernacular string `parquet:"vernacular"`
 Years []int32 `parquet:"years,list"`;Countries []string `parquet:"countries,list"`;Ambiguous bool `parquet:"country_ambiguous"`
 Payload string `parquet:"private_record_json"`;RecordSHA string `parquet:"record_sha256"`;SourceFile string `parquet:"source_file,dict"`;SourceSHA string `parquet:"source_sha256,dict"`;Ordinal uint64 `parquet:"record_ordinal"`
}
type File struct{Name,Path,SHA string;Bytes int64}
type Summary struct {Name,SourcePath,SourceSHA,ToolSHA,RulesSHA,Path,SHA string;Records,CountryRecords,BookRecords,Deletes uint64;Bytes int64;Seconds float64}
func must(e error){if e!=nil{panic(e)}}
func save(p string,v any){b,e:=json.MarshalIndent(v,"","  ");must(e);t:=p+".partial";must(os.WriteFile(t,append(b,'\n'),0600));must(os.Rename(t,p))}
func hash(path string)string{f,e:=os.Open(path);must(e);defer f.Close();h:=sha256.New();_,e=io.Copy(h,f);must(e);return hex.EncodeToString(h.Sum(nil))}
func parse(f File,out,tool,rules string)Summary{
 started:=time.Now();name:=f.Name;p:=filepath.Join(out,name+".parquet");receipt:=p+".json";var old Summary
 if b,e:=os.ReadFile(receipt);e==nil&&json.Unmarshal(b,&old)==nil&&old.SourceSHA==f.SHA&&old.ToolSHA==tool&&old.RulesSHA==rules&&old.SHA==hash(old.Path){return old}
 input,e:=os.Open(f.Path);must(e);defer input.Close();st,e:=input.Stat();must(e);if st.Size()!=f.Bytes{panic("source byte mismatch")}
 rawSHA:=sha256.New();gz,e:=gzip.NewReader(io.TeeReader(input,rawSHA));must(e);defer gz.Close();dec:=xml.NewDecoder(bufio.NewReaderSize(gz,1<<20));dec.Strict=false
 output,e:=os.OpenFile(p+".partial",os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0600);must(e)
 w:=parquet.NewGenericWriter[PoolRow](output,parquet.Compression(&zstd.Codec{Level:zstd.SpeedFastest,Concurrency:1}),parquet.MaxRowsPerRowGroup(30000))
 s:=Summary{Name:name,SourcePath:f.Path,SourceSHA:f.SHA,ToolSHA:tool,RulesSHA:rules,Path:p};rows:=make([]PoolRow,0,256)
 flush:=func(){if len(rows)>0{_,e:=w.Write(rows);must(e);rows=rows[:0]}}
 for {tok,e:=dec.Token();if e==io.EOF{break};must(e);st,ok:=tok.(xml.StartElement);if !ok{continue}
 switch st.Name.Local{case "PubmedArticle":var a Article;must(dec.DecodeElement(&a,&st));r:=convert(a);s.Records++
  years:=[]int32{};for _,y:=range r.Years{years=append(years,int32(y))};payload,rsha:="","";if len(r.Countries)>0{b,e:=json.Marshal(r);must(e);payload=string(b);h:=sha256.Sum256(b);rsha=hex.EncodeToString(h[:]);s.CountryRecords++}
  rows=append(rows,PoolRow{r.PMID,r.DOI,r.PMCID,r.Title,r.Vernacular,years,r.Countries,r.CountryAmbiguous,payload,rsha,name,f.SHA,s.Records});if len(rows)==cap(rows){flush()}
 case "PubmedBookArticle":s.BookRecords++;must(dec.Skip())
 case "DeleteCitation":s.Deletes++;must(dec.Skip())}
 }
 flush();must(w.Close());must(output.Close());if actual:=hex.EncodeToString(rawSHA.Sum(nil));actual!=f.SHA{panic("raw compressed source SHA mismatch "+name+" "+actual)}
 must(os.Rename(p+".partial",p));s.SHA=hash(p);st,e=os.Stat(p);must(e);s.Bytes=st.Size();s.Seconds=time.Since(started).Seconds();save(receipt,s);return s
}
func main(){
 plan:=flag.String("plan","","frozen source-v1.json");oldDir:=flag.String("old","","old verified205 directory");newDir:=flag.String("new","","new verified1129 directory");out:=flag.String("out","","private source pool");rule:=flag.String("rules","","frozen country aliases");workers:=flag.Int("workers",16,"local parser concurrency");watch:=flag.Bool("watch",true,"wait for all declared source files");flag.Parse()
 must(os.MkdirAll(*out,0700));must(os.Chmod(*out,0700));b,e:=os.ReadFile(*rule);must(e);var rs struct{Aliases []struct{Name,Code string}};must(json.Unmarshal(b,&rs));aliases=map[string]string{};for _,a:=range rs.Aliases{aliases[norm(a.Name)]=a.Code}
 rulesSHA:=hash(*rule);exe,e:=os.Executable();must(e);tool:=hash(exe)
 b,e=os.ReadFile(*plan);must(e);var source struct{PubMed struct{Files []struct{Name string `json:"name"`;Bytes int64 `json:"source_bytes"`} `json:"files"`} `json:"pubmed"`};must(json.Unmarshal(b,&source));if len(source.PubMed.Files)!=1334{panic("source baseline count")}
 seen:=map[string]bool{};jobs:=make(chan File,*workers);done:=make(chan Summary,*workers);var wg sync.WaitGroup;for i:=0;i<*workers;i++{wg.Add(1);go func(){defer wg.Done();for f:=range jobs{done<-parse(f,*out,tool,rulesSHA)}}()}
 started:=time.Now();results:=[]Summary{};nextScan:=time.NewTicker(3*time.Second);defer nextScan.Stop();scheduled:=0
 schedule:=func(){for _,f:=range source.PubMed.Files{if seen[f.Name]{continue};for _,d:=range []string{*oldDir,*newDir}{p:=filepath.Join(d,f.Name);b,e:=os.ReadFile(p+".bridge-verified.json");if e!=nil{continue};var receipt struct{Status,SHA,MD5 string;Bytes int64};var raw map[string]any;must(json.Unmarshal(b,&raw));receipt.Status,_=raw["status"].(string);receipt.SHA,_=raw["sha256"].(string);receipt.MD5,_=raw["md5"].(string);n,_:=raw["bytes"].(float64);receipt.Bytes=int64(n)
   if receipt.Status!="passed"||len(receipt.SHA)!=64||len(receipt.MD5)!=32||receipt.Bytes!=f.Bytes{panic("invalid verified source receipt")};seen[f.Name]=true;scheduled++;jobs<-File{f.Name,p,receipt.SHA,receipt.Bytes};break}}}
 // Feed sources in a separate goroutine so a full jobs channel cannot block results.
 feedDone:=make(chan struct{});go func(){defer close(feedDone);for{schedule();if scheduled==1334||!*watch{close(jobs);return};time.Sleep(3*time.Second)}}()
 go func(){<-feedDone;wg.Wait();close(done)}()
 for s:=range done{results=append(results,s);if len(results)%20==0{fmt.Printf("parsed=%d/1334 records=%d seconds=%.1f\n",len(results),s.Records,time.Since(started).Seconds())}}
 sort.Slice(results,func(i,j int)bool{return results[i].Name<results[j].Name});var n,country,books uint64;for _,s:=range results{n+=s.Records;country+=s.CountryRecords;books+=s.BookRecords}
 status:="partial_source_preparation";if len(results)==1334{status="complete_frozen_baseline_source_preparation"};save(filepath.Join(*out,"manifest.json"),map[string]any{"status":status,"files":results,"file_count":len(results),"records":n,"country_records":country,"book_records_unimplemented":books,"source_scope":"complete Jan2026 baseline-only;updates not applied","tool_sha256":tool,"rules_sha256":rulesSHA,"source_plan_sha256":hash(*plan),"seconds":time.Since(started).Seconds(),"source_mutated":false,"work_country_patch_generated":false,"private_payload_contains_original_affiliation_contact_text":true});fmt.Printf("DONE files=%d records=%d country_records=%d seconds=%.1f\n",len(results),n,country,time.Since(started).Seconds())
}
