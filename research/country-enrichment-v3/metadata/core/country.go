package core

import (
 "encoding/json"
 "os"
 "regexp"
 "sort"
 "strings"
)
var email=regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
var emailTail=regexp.MustCompile(`(?i)(?:[.;,]\s*)?(?:electronic address|e-mail|email)\s*:.*$`)
func Redact(s string)string{return email.ReplaceAllString(s,"[email removed]")}
type CountryRules struct{Aliases map[string]string}
func LoadRules(path string)(CountryRules,error){b,e:=os.ReadFile(path);if e!=nil{return CountryRules{},e};var x struct{Aliases []struct{Name,Code string}};if e=json.Unmarshal(b,&x);e!=nil{return CountryRules{},e};r:=CountryRules{map[string]string{}};for _,a:=range x.Aliases{r.Aliases[countryNorm(a.Name)]=a.Code};return r,nil}
func countryNorm(s string)string{return strings.ToLower(strings.Join(strings.Fields(strings.Trim(s," \t\r\n.()[]{}\"'"))," "))}
func (r CountryRules)Countries(s string)([]string,bool){
 s=emailTail.ReplaceAllString(s,"");s=email.ReplaceAllString(s,"");s=strings.TrimRight(s," ;,\t\r\n")
 ps:=strings.FieldsFunc(s,func(c rune)bool{return c==','||c==';'||c=='\n'||c=='\r'||c=='，'||c=='；'})
 if len(ps)==0{return nil,false};last:=countryNorm(ps[len(ps)-1]);if r.Aliases[last]==""{return nil,last=="korea"||last=="congo"||last=="georgia"}
 set:=map[string]bool{};for _,p:=range ps[max(0,len(ps)-3):]{if c:=r.Aliases[countryNorm(p)];c!=""{set[c]=true}}
 out:=[]string{};for c:=range set{out=append(out,c)};sort.Strings(out);return out,len(out)>1
}
