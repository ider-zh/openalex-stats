// Prepare read-only official PMC bulk source indexes, without Work predictions.
package main

import (
 "compress/gzip"
 "crypto/md5"
 "crypto/sha256"
 "encoding/csv"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "sort"
 "strings"
 "sync"
 "time"
 "github.com/parquet-go/parquet-go"
 "github.com/parquet-go/parquet-go/compress/zstd"
)
type sourceRow struct {
 PMCID string `parquet:"pmcid"`
 DOI string `parquet:"doi"`
 PMID string `parquet:"pmid"`
 InOAInventory bool `parquet:"in_oa_inventory"`
}
type inventoryRow struct {
 PMCID string `parquet:"pmcid"`
 MetadataKeys []string `parquet:"metadata_keys,list"`
}
type item struct{Key string `json:"key"`;MD5 string `json:"MD5checksum"`}
func must(e error){if e!=nil{panic(e)}}
func save(p string,x any){b,e:=json.MarshalIndent(x,"","  ");must(e);must(os.WriteFile(p,append(b,'\n'),0600))}
func normDOI(s string)string{
 s=strings.TrimSpace(strings.ToLower(s));for _,p:=range []string{"https://doi.org/","http://doi.org/","https://dx.doi.org/","doi:"}{s=strings.TrimPrefix(s,p)};return strings.TrimSpace(s)
}
func main(){
 if len(os.Args)!=4{panic("usage: index old-fulltext-root outputdir pmc-idmap-file")};old,out,idmap:=os.Args[1],os.Args[2],os.Args[3];must(os.MkdirAll(out,0700));start:=time.Now()
 var plan struct{Files []item `json:"files"`};b,e:=os.ReadFile(filepath.Join(old,"pmc-inventory-plan.json"));must(e);must(json.Unmarshal(b,&plan));planSHA:=sha256.Sum256(b)
 combined:=map[string][]string{};type invPart struct{Data map[string][]string;File string;MD5 string};jobs:=make(chan item);done:=make(chan invPart);var wg sync.WaitGroup
 for i:=0;i<8;i++{wg.Add(1);go func(){defer wg.Done();for it:=range jobs{
  p:=filepath.Join(old,"inventory",filepath.Base(it.Key));f,e:=os.Open(p);must(e);h:=md5.New();g,e:=gzip.NewReader(io.TeeReader(f,h));must(e);r:=csv.NewReader(g);r.FieldsPerRecord=-1;m:=map[string][]string{}
  for{row,e:=r.Read();if e==io.EOF{break};must(e);if len(row)<2{continue};key:=row[1];name:=filepath.Base(key);pmc:=strings.Split(name,".")[0];if strings.HasPrefix(pmc,"PMC"){m[pmc]=append(m[pmc],key)}}
  g.Close();io.Copy(h,f);f.Close();sum:=hex.EncodeToString(h.Sum(nil));if sum!=it.MD5{panic("official inventory MD5 mismatch "+p)};done<-invPart{m,p,sum}
 }}()};go func(){for _,it:=range plan.Files{jobs<-it};close(jobs);wg.Wait();close(done)}();receipts:=[]map[string]any{}
 for p:=range done{for id,keys:=range p.Data{combined[id]=append(combined[id],keys...)};receipts=append(receipts,map[string]any{"path":p.File,"md5":p.MD5,"pmcid_count":len(p.Data)})}
 fmt.Printf("inventory verified: files=%d unique_pmcid=%d elapsed=%.2f\n",len(receipts),len(combined),time.Since(start).Seconds())
 invPath:=filepath.Join(out,"pmc-oa-inventory.parquet");invFile,e:=os.Create(invPath+".tmp");must(e);iw:=parquet.NewGenericWriter[inventoryRow](invFile,parquet.Compression(&zstd.Codec{Level:zstd.SpeedFastest}),parquet.MaxRowsPerRowGroup(65536));ids:=make([]string,0,len(combined));for id:=range combined{ids=append(ids,id)};sort.Strings(ids);ib:=make([]inventoryRow,0,4096)
 for _,id:=range ids{sort.Strings(combined[id]);ib=append(ib,inventoryRow{id,combined[id]});if len(ib)==cap(ib){_,e=iw.Write(ib);must(e);ib=ib[:0]}};if len(ib)>0{_,e=iw.Write(ib);must(e)};must(iw.Close());must(invFile.Close());must(os.Rename(invPath+".tmp",invPath))
 srcPath:=filepath.Join(out,"pmc-source-idmap.parquet");srcFile,e:=os.Create(srcPath+".tmp");must(e);sw:=parquet.NewGenericWriter[sourceRow](srcFile,parquet.Compression(&zstd.Codec{Level:zstd.SpeedFastest}),parquet.MaxRowsPerRowGroup(65536));f,e:=os.Open(idmap);must(e);sourceHash:=sha256.New();g,e:=gzip.NewReader(io.TeeReader(f,sourceHash));must(e);r:=csv.NewReader(g);r.FieldsPerRecord=-1;header,e:=r.Read();must(e);ix:=map[string]int{};for i,k:=range header{ix[k]=i};for _,key:=range []string{"PMCID","DOI","PMID"}{if _,ok:=ix[key];!ok{panic("source missing column "+key)}}
 count:=uint64(0);sb:=make([]sourceRow,0,8192)
 for{row,e:=r.Read();if e==io.EOF{break};must(e);count++;if len(row)<len(header){panic("short ID map record")};pmc:=strings.ToUpper(strings.TrimSpace(row[ix["PMCID"]]));_,oa:=combined[pmc];sb=append(sb,sourceRow{pmc,normDOI(row[ix["DOI"]]),strings.TrimSpace(row[ix["PMID"]]),oa});if len(sb)==cap(sb){_,e=sw.Write(sb);must(e);sb=sb[:0]}}
 if len(sb)>0{_,e=sw.Write(sb);must(e)};must(sw.Close());must(srcFile.Close());g.Close();io.Copy(sourceHash,f);f.Close();must(os.Rename(srcPath+".tmp",srcPath));sourceSHA:=hex.EncodeToString(sourceHash.Sum(nil));if sourceSHA!="3c12867d7abf2c41cd2bbf10ca87a84a8aa7dc796609611154b699741a281fe8"{panic("frozen PMC source SHA mismatch")}
 save(filepath.Join(out,"source-index-acceptance.json"),map[string]any{"status":"source_preparation_complete_not_work_predictions","official_inventory_files":receipts,"inventory_pmcids":len(ids),"source_rows":count,"source_idmap_sha256":sourceSHA,"inventory_plan_sha256":hex.EncodeToString(planSHA[:]),"elapsed_seconds":time.Since(start).Seconds(),"original_mutated":false,"work_gate_applied":false});fmt.Printf("done source_rows=%d elapsed=%.2f\n",count,time.Since(start).Seconds())
}
