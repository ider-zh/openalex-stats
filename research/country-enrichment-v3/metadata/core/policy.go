// Package core implements the pre-holdout frozen metadata identity rules.
package core

import (
 "regexp"
 "strings"
 "unicode"
)

type OriginalAuthor struct { Name string `json:"name"`; RawName string `json:"raw_author_name"`; ORCID string `json:"orcid"`; RawORCID string `json:"raw_orcid"` }
type SourceAuthor struct { Given string `json:"given"`; Family string `json:"family"`; ORCIDs []string `json:"orcids"`; Affiliations []string `json:"affiliations"` }
type OriginalWork struct { ID uint64 `json:"id"`; Title string `json:"title"`; DOI string `json:"doi"`; PMID string `json:"pmid"`; Year int `json:"year"`; Authors []OriginalAuthor `json:"authors"` }
type IdentityResult struct { Pass bool `json:"pass"`; Reason string `json:"reason"`; AuthorMatch string `json:"author_match"` }

func DOI(s string)string { s=strings.ToLower(strings.TrimSpace(s));for _,p:=range []string{"https://doi.org/","http://doi.org/","https://dx.doi.org/","http://dx.doi.org/","doi:"}{s=strings.TrimPrefix(s,p)};return strings.TrimSpace(s) }
func Normalize(s string)string {var b strings.Builder;for _,r:=range s{if unicode.IsLetter(r)||unicode.IsDigit(r){b.WriteRune(unicode.ToLower(r))}else{b.WriteByte(' ')}};return strings.Join(strings.Fields(b.String())," ")}
func cjk(s string)int{n:=0;for _,r:=range s{if unicode.Is(unicode.Han,r){n++}};return n}
func letters(s string)int{n:=0;for _,r:=range s{if unicode.IsLetter(r)||unicode.IsDigit(r){n++}};return n}
func LongTitle(s string)bool {n:=Normalize(s);return (letters(n)>=20&&len(strings.Fields(n))>=5)||cjk(n)>=10}
var orcidShape=regexp.MustCompile(`^[0-9]{4}-[0-9]{4}-[0-9]{4}-[0-9]{3}[0-9X]$`)
func ORCID(s string)string {
 s=strings.ToUpper(strings.TrimSpace(s));for _,p:=range []string{"HTTPS://ORCID.ORG/","HTTP://ORCID.ORG/"}{s=strings.TrimPrefix(s,p)}
 if !orcidShape.MatchString(s){return ""};d:=strings.ReplaceAll(s,"-","");total:=0
 for i:=0;i<15;i++{total=(total+int(d[i]-'0'))*2};check:=(12-total%11)%11;want:=byte('X');if check<10{want=byte('0'+check)};if d[15]!=want{return ""};return s
}
func originalName(a OriginalAuthor)string{if strings.TrimSpace(a.RawName)!=""{return Normalize(a.RawName)};return Normalize(a.Name)}
func nameSame(o OriginalAuthor,s SourceAuthor)bool {n:=originalName(o);return n!=""&&(n==Normalize(s.Given+" "+s.Family)||n==Normalize(s.Family+" "+s.Given))}
func exactORCID(o OriginalAuthor,s SourceAuthor)bool {for _,a:=range []string{o.RawORCID,o.ORCID}{a=ORCID(a);if a==""{continue};for _,b:=range s.ORCIDs{if a==ORCID(b){return true}}};return false}
func strongName(o OriginalAuthor,s SourceAuthor)bool {
 if !nameSame(o,s){return false};n:=originalName(o);if cjk(n)>=3{return true}
 return len(strings.Fields(n))>=2&&letters(n)>=8&&letters(s.Given)>=2&&letters(s.Family)>=2
}
func CrossrefIdentity(w OriginalWork,sourceDOI,relationship,role string,s SourceAuthor)IdentityResult {
 if DOI(w.DOI)==""||DOI(w.DOI)!=DOI(sourceDOI){return IdentityResult{Reason:"doi_missing_or_conflict"}}
 if relationship!="contributor-affiliation"||role!="author"{return IdentityResult{Reason:"not_personal_author_affiliation"}}
 if strings.TrimSpace(s.Given)==""||strings.TrimSpace(s.Family)==""{return IdentityResult{Reason:"source_author_name_missing"}}
 for _,o:=range w.Authors{if exactORCID(o,s)&&nameSame(o,s){return IdentityResult{true,"strict_personal_author_identity","exact_valid_orcid_and_original_name"}}}
 return IdentityResult{Reason:"source_orcid_and_same_original_name_not_supported"}
}
func PubMedIdentity(w OriginalWork,sourcePMID,sourceDOI,title,vernacular string,years []int,authors []SourceAuthor)IdentityResult {
 if w.PMID!=""&&w.PMID!=sourcePMID{return IdentityResult{Reason:"pmid_conflict"}}
 if DOI(w.DOI)!=""&&DOI(sourceDOI)!=""&&DOI(w.DOI)!=DOI(sourceDOI){return IdentityResult{Reason:"doi_conflict"}}
 if !(w.PMID!=""&&w.PMID==sourcePMID||DOI(w.DOI)!=""&&DOI(w.DOI)==DOI(sourceDOI)){return IdentityResult{Reason:"no_exact_identifier"}}
 if !LongTitle(w.Title)||!(Normalize(w.Title)==Normalize(title)||Normalize(w.Title)==Normalize(vernacular)){return IdentityResult{Reason:"strict_long_title_not_equal"}}
 close:=false;for _,y:=range years{if w.Year>0&&y>=w.Year-1&&y<=w.Year+1{close=true}};if !close{return IdentityResult{Reason:"publication_year_not_supported"}}
 for _,o:=range w.Authors{for _,s:=range authors{if exactORCID(o,s){return IdentityResult{true,"strict_pubmed_identity","exact_valid_orcid"}};if strongName(o,s){return IdentityResult{true,"strict_pubmed_identity","exact_original_full_name"}}}}
 return IdentityResult{Reason:"original_author_not_supported"}
}
