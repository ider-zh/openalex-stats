// Independent review gate. Never infers a gold label from a route prediction.
package main

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "math"
 "os"
 "sort"
)
type Review struct {
 ID uint64 `json:"id"`
 Route string `json:"route"`
 Selected bool `json:"selected_for_audit"`
 Identity string `json:"identity_status"`
 CountrySupported *bool `json:"all_predicted_countries_source_supported"`
 OriginalSource string `json:"original_source_path"`
 OriginalSourceSHA string `json:"original_source_sha256"`
 Evidence string `json:"independent_evidence"`
 VisibleComplete *bool `json:"source_visible_country_set_complete"`
 FullPaperComplete *bool `json:"full_paper_country_set_complete"`
 SystemicError bool `json:"unisolated_systemic_error"`
}
type Gate struct {
 Route string `json:"route"`
 Selected,Successes,UnverifiedOrIncorrect,VisibleCompletenessObserved,VisibleComplete int
 FullPaperCompletenessObserved,FullPaperComplete int
 Precision,WilsonLower,WilsonUpper *float64
 Status,Reason string
 SystemicError bool
 HumanUserReviewComplete bool `json:"human_user_review_complete"`
 ProductionApplied bool `json:"production_applied"`
}
func wilson(success,n int)(float64,float64){
 if n<=0||success<0||success>n{panic("invalid binomial counts")}
 z:=1.959963984540054; nn:=float64(n);p:=float64(success)/nn
 center:=(p+z*z/(2*nn))/(1+z*z/nn)
 radius:=z*math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn))/(1+z*z/nn)
 return center-radius,center+radius
}
func evaluate(route string,rows []Review) Gate {
 g:=Gate{Route:route,Status:"insufficient_n",Reason:"At least128 independent, nondevelopment selected candidate Works required"}
 seen:=map[uint64]bool{}
 for _,r:=range rows{
  if r.Route!=route||!r.Selected{continue};if r.ID==0||seen[r.ID]{panic("zero/duplicate candidate Work in route audit")};seen[r.ID]=true;g.Selected++
  validEvidence:=r.OriginalSource!=""&&len(r.OriginalSourceSHA)==64&&r.Evidence!=""
  _,err:=hex.DecodeString(r.OriginalSourceSHA);validEvidence=validEvidence&&err==nil
  if r.Identity=="verified"&&r.CountrySupported!=nil&&*r.CountrySupported&&validEvidence{g.Successes++}
  if r.VisibleComplete!=nil{g.VisibleCompletenessObserved++;if *r.VisibleComplete{g.VisibleComplete++}}
  if r.FullPaperComplete!=nil{g.FullPaperCompletenessObserved++;if *r.FullPaperComplete{g.FullPaperComplete++}}
  g.SystemicError=g.SystemicError||r.SystemicError
 }
 g.UnverifiedOrIncorrect=g.Selected-g.Successes
 if g.Selected>0{p:=float64(g.Successes)/float64(g.Selected);lo,hi:=wilson(g.Successes,g.Selected);g.Precision=&p;g.WilsonLower=&lo;g.WilsonUpper=&hi}
 if g.Selected<128{return g}
 if g.SystemicError{g.Status="blocked_systemic_error";g.Reason="Unisolated systematic identity/role/reference leakage forbids rollout";return g}
 if *g.Precision<.99||*g.WilsonLower<.97{g.Status="failed_precision";g.Reason="Requires point precision>=0.99 and two-sided95%Wilson lower>=0.97; unverified stays in denominator";return g}
 g.Status="passed_source_supported_country_precision";g.Reason="Gate supports predicted author-country labels, not complete worldwide author affiliations; completeness remains separately qualified"
 return g
}
func main(){
 if len(os.Args)!=3{panic("usage: audit independent-reviews.json gate-output.json")}
 raw,err:=os.ReadFile(os.Args[1]);if err!=nil{panic(err)}
 var in struct{Routes []string `json:"declared_routes"`;Reviews []Review `json:"reviews"`};if err=json.Unmarshal(raw,&in);err!=nil{panic(err)}
 routes:=map[string]bool{};for _,name:=range in.Routes{routes[name]=true};for _,r:=range in.Reviews{routes[r.Route]=true};names:=[]string{};for r:=range routes{names=append(names,r)};sort.Strings(names)
 gates:=[]Gate{};for _,r:=range names{gates=append(gates,evaluate(r,in.Reviews))}
 sha:=sha256.Sum256(raw);out:=map[string]any{"status":"independent_route_gate_evaluated","review_sha256":hex.EncodeToString(sha[:]),"routes":gates,"production_applied":false,"user_review_complete":false}
 b,err:=json.MarshalIndent(out,"","  ");if err!=nil{panic(err)};if err=os.WriteFile(os.Args[2],append(b,'\n'),0600);err!=nil{panic(err)};fmt.Println(string(b))
}
