package main
import("fmt";"os";"path/filepath";"testing";"github.com/parquet-go/parquet-go")
func TestMissingMapKeysDoNotLeakAcrossRawReadBatches(t *testing.T){
 d:=t.TempDir();raw:=filepath.Join(d,"raw.parquet");tp:=filepath.Join(d,"target.parquet");s:=func(x string)*string{return &x};y:=int32(2020)
 src:=make([]Raw,2049);for i:=range src{src[i]=Raw{ID:s(fmt.Sprintf("https://openalex.org/W%d",i+1)),Year:&y,Type:s("article"),Title:s("A sufficiently long title for this test"),IDs:map[string]*string{"openalex":s(fmt.Sprintf("https://openalex.org/W%d",i+1))}}};src[0].IDs["pmid"]=s("https://pubmed.ncbi.nlm.nih.gov/12345");src[0].IDs["doi"]=s("https://doi.org/10.1000/first-only")
 if e:=parquet.WriteFile(raw,src);e!=nil{t.Fatal(e)};targets:=[]Target{{ID:1,Year:y,Type:"article",Row:0},{ID:2049,Ordinal:2048,Year:y,Type:"article",Row:2048}};if e:=parquet.WriteFile(tp,targets);e!=nil{t.Fatal(e)}
 out:=filepath.Join(d,"out");os.Mkdir(out,0700);r:=run(Part{Index:0,RawPath:raw,Path:tp,SHA:hash(tp),Rows:2,Core:2},out,"synthetic")
 got,e:=parquet.ReadFile[Row](r.Path);if e!=nil{t.Fatal(e)};if len(got)!=2||got[0].PMID!="12345"||got[0].DOI!="10.1000/first-only"||got[1].PMID!=""||got[1].RawPMID!=""||got[1].DOI!=""||got[1].RawDOI!=""{t.Fatalf("missing map key inherited identifiers: %+v",got)}
}
