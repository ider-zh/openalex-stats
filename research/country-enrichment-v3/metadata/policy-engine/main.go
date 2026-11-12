// Streaming, frozen identity/country evaluator. No network access.
package main
import (
 "bufio"
 "encoding/json"
 "flag"
 "fmt"
 "os"
 "sort"
 "openalexstats/research/country-enrichment-v3/metadata/core"
)
type source struct{
 PMID string `json:"pmid"`; DOI string `json:"doi"`; Title string `json:"title"`; Vernacular string `json:"vernacular"`;Years []int `json:"years"`
 Authors []core.SourceAuthor `json:"authors"`;Author core.SourceAuthor `json:"author"`;LegacyAffiliation string `json:"legacy_affiliation"`
 Relationship string `json:"relationship"`;Role string `json:"role"`
}
type input struct{Route string `json:"route"`;Original core.OriginalWork `json:"original"`;Source source `json:"source"`;Evidence any `json:"evidence"`}
func main(){
 aliases:=flag.String("aliases","","frozen aliases");flag.Parse();rules,e:=core.LoadRules(*aliases);if e!=nil{panic(e)}
 sc:=bufio.NewScanner(os.Stdin);sc.Buffer(make([]byte,65536),16<<20);enc:=json.NewEncoder(os.Stdout)
 for sc.Scan(){var in input;if e=json.Unmarshal(sc.Bytes(),&in);e!=nil{panic(e)};var identity core.IdentityResult;affs:=[]string{}
 switch in.Route{case "crossref":identity=core.CrossrefIdentity(in.Original,in.Source.DOI,in.Source.Relationship,in.Source.Role,in.Source.Author);affs=in.Source.Author.Affiliations
 case "pubmed":identity=core.PubMedIdentity(in.Original,in.Source.PMID,in.Source.DOI,in.Source.Title,in.Source.Vernacular,in.Source.Years,in.Source.Authors);for _,a:=range in.Source.Authors{affs=append(affs,a.Affiliations...)};if in.Source.LegacyAffiliation!=""{affs=append(affs,in.Source.LegacyAffiliation)}
 default:panic("unsupported route")}
 set:=map[string]bool{};ambiguous:=false;for _,a:=range affs{cs,amb:=rules.Countries(a);ambiguous=ambiguous||amb;if !amb&&identity.Pass{for _,c:=range cs{set[c]=true}}}
 countries:=[]string{};for c:=range set{countries=append(countries,c)};sort.Strings(countries);complete:="unknown";if ambiguous{complete="false"}
 out:=map[string]any{"id":in.Original.ID,"route":in.Route,"identity":identity,"original_countries":[]string{},"proposed_countries":countries,"country_set_complete":complete,"source_country_ambiguous":ambiguous,"eligible":identity.Pass&&len(countries)>0,"evidence":in.Evidence}
 if e=enc.Encode(out);e!=nil{panic(e)}
 };if sc.Err()!=nil{panic(sc.Err())};fmt.Fprintln(os.Stderr,"frozen policy evaluation complete")
}
