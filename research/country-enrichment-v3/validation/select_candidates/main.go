// Frozen hash sample from an externally frozen unique-Work candidate stream.
package main
import("bufio";"container/heap";"crypto/sha256";"encoding/hex";"encoding/json";"fmt";"os";"sort";"strconv";"strings")
type point struct{ID uint64;H [32]byte;Raw json.RawMessage}
type maximum []point
func(h maximum)Len()int{return len(h)}
func(h maximum)Less(i,j int)bool{return string(h[i].H[:])>string(h[j].H[:])||(h[i].H==h[j].H&&h[i].ID>h[j].ID)}
func(h maximum)Swap(i,j int){h[i],h[j]=h[j],h[i]}
func(h *maximum)Push(x any){*h=append(*h,x.(point))}
func(h *maximum)Pop()any{a:=*h;x:=a[len(a)-1];*h=a[:len(a)-1];return x}
func must(e error){if e!=nil{panic(e)}}
func idOf(row map[string]json.RawMessage)uint64{
 for _,k:=range []string{"id","work_id"}{b:=row[k];if len(b)==0{continue};var n uint64;if json.Unmarshal(b,&n)==nil&&n>0{return n};var s string;if json.Unmarshal(b,&s)==nil{s=strings.TrimPrefix(s,"https://openalex.org/");s=strings.TrimPrefix(s,"W");n,e:=strconv.ParseUint(s,10,64);if e==nil&&n>0{return n}}};panic("candidate missing Work identity")
}
func main(){
 if len(os.Args)!=5{panic("usage: select_candidates input.jsonl route exclusions.json output.json")}
 in,route,excludedPath,out:=os.Args[1],os.Args[2],os.Args[3],os.Args[4]
 b,e:=os.ReadFile(excludedPath);must(e);var ex struct{IDs []uint64 `json:"ids"`};must(json.Unmarshal(b,&ex));excluded:=map[uint64]bool{};for _,id:=range ex.IDs{excluded[id]=true}
 f,e:=os.Open(in);must(e);defer f.Close();scanner:=bufio.NewScanner(f);scanner.Buffer(make([]byte,1<<20),32<<20)
 const seed="openalex-country-enrichment-v3-20261112-route-candidate-audit-01"
 seen:=map[uint64][32]byte{};h:=maximum{};lines,excludedCount,duplicateIdentical:=0,0,0
 for scanner.Scan(){raw:=append([]byte(nil),scanner.Bytes()...);if len(strings.TrimSpace(string(raw)))==0{continue};lines++;var row map[string]json.RawMessage;must(json.Unmarshal(raw,&row));id:=idOf(row)
  canonical,e:=json.Marshal(row);must(e);sh:=sha256.Sum256(canonical)
  if old,ok:=seen[id];ok{if old!=sh{panic(fmt.Sprintf("conflicting duplicate Work %d; candidate stream must already be deduplicated",id))};duplicateIdentical++;continue};seen[id]=sh
  if excluded[id]{excludedCount++;continue}
  hh:=sha256.Sum256([]byte(seed+"\n"+route+"\n"+strconv.FormatUint(id,10)));p:=point{id,hh,raw}
  if len(h)<128{heap.Push(&h,p)}else if string(hh[:])<string(h[0].H[:])||(hh==h[0].H&&id<h[0].ID){h[0]=p;heap.Fix(&h,0)}
 };must(scanner.Err());sort.Slice(h,func(i,j int)bool{return string(h[i].H[:])<string(h[j].H[:])||(h[i].H==h[j].H&&h[i].ID<h[j].ID)})
 records:=[]map[string]any{};for _,p:=range h{records=append(records,map[string]any{"id":p.ID,"route":route,"audit_hash_sha256":hex.EncodeToString(p.H[:]),"candidate":p.Raw})}
 eligible:=len(seen)-excludedCount;pi:=0.;if eligible>0{pi=float64(len(h))/float64(eligible)}
 x:=map[string]any{"status":"frozen_non_development_candidate_audit_sample","seed":seed,"hash":"SHA256(seed+newline+route+newline+decimalWorkID)","route":route,"input_jsonl":in,"input_lines":lines,"unique_candidate_work_ids":len(seen),"excluded_development_candidates":excludedCount,"identical_duplicates_ignored":duplicateIdentical,"eligible_candidate_frame_N":eligible,"sample_n":len(h),"inclusion_probability":pi,"sample_size_cap":128,"insufficient_n":len(h)<128,"records":records,"selection_after_outcome_rule":"never replace failed/unverified audits","gold_generated":false,"production_applied":false}
 b,e=json.MarshalIndent(x,"","  ");must(e);must(os.WriteFile(out,append(b,'\n'),0600));fmt.Printf("route=%s frame=%d sample=%d excluded=%d\n",route,eligible,len(h),excludedCount)
}
