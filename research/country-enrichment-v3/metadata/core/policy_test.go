package core
import "testing"
func TestCrossrefRequiresSameRawNameAndValidORCID(t *testing.T){
 w:=OriginalWork{DOI:"https://doi.org/10.1/a",Authors:[]OriginalAuthor{{Name:"Wrong merged profile",RawName:"True Person",ORCID:"0000-0002-1825-0097"}}}
 s:=SourceAuthor{Given:"Different",Family:"Person",ORCIDs:[]string{"0000-0002-1825-0097"}}
 if CrossrefIdentity(w,"10.1/a","contributor-affiliation","author",s).Pass{t.Fatal("profile ORCID overrode conflicting raw name")}
 s.Given="True";if !CrossrefIdentity(w,"10.1/a","contributor-affiliation","author",s).Pass{t.Fatal("exact same person refused")}
 if CrossrefIdentity(w,"10.1/a","institutional-contributor","author",s).Pass{t.Fatal("institution author accepted")}
 s.ORCIDs=[]string{"0000-0002-1825-0098"};if CrossrefIdentity(w,"10.1/a","contributor-affiliation","author",s).Pass{t.Fatal("invalid check digit accepted")}
}
func TestPubMedIdentityConflictsCannotBeMasked(t *testing.T){
 w:=OriginalWork{DOI:"10.1/a",PMID:"12",Year:2020,Title:"A comprehensive analysis of biological systems",Authors:[]OriginalAuthor{{RawName:"Claudia Researcher"}}};s:=[]SourceAuthor{{Given:"Claudia",Family:"Researcher"}}
 if !PubMedIdentity(w,"12","10.1/a",w.Title,"",[]int{2021},s).Pass{t.Fatal("strict positive rejected")}
 if PubMedIdentity(w,"12","10.1/wrong",w.Title,"",[]int{2021},s).Pass{t.Fatal("DOI conflict accepted")}
 if PubMedIdentity(w,"12","10.1/a","A different paper involving other systems","",[]int{2021},s).Pass{t.Fatal("wrong title accepted")}
 w.Title="Poland";if PubMedIdentity(w,"12","10.1/a","Poland","",[]int{2021},s).Pass{t.Fatal("short title bypass")}
}
func TestCountryTailsNotJournalCountryOrInstitutionBrand(t *testing.T){
 r:=CountryRules{map[string]string{"china":"CN","hong kong":"HK","united states":"US"}}
 if c,a:=r.Countries("Department, China. Electronic address: person@sample.cn");a||len(c)!=1||c[0]!="CN"{t.Fatal(c,a)}
 if c,_:=r.Countries("Institute of China Studies, Cambridge");len(c)>0{t.Fatal("brand guessed")}
 if _,a:=r.Countries("Department, Hong Kong, China");!a{t.Fatal("jurisdiction conflict not retained")}
}
