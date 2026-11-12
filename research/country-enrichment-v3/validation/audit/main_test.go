package main
import("testing";"math";"strings")
func truep()*bool{x:=true;return &x}
func fixtures(n int)[]Review{a:=make([]Review,n);for i:=range a{a[i]=Review{ID:uint64(i+1),Route:"r",Selected:true,Identity:"verified",CountrySupported:truep(),OriginalSource:"original.xml",OriginalSourceSHA:strings.Repeat("a",64),Evidence:"Independently inspected author-role affiliation and source identity"}};return a}
func TestWilsonAllSuccessClosedForm(t *testing.T){lo,hi:=wilson(128,128);z:=1.959963984540054;want:=128/(128+z*z);if math.Abs(lo-want)>1e-14||math.Abs(hi-1)>1e-14{t.Fatal(lo,hi,want)}}
func TestGateMinimumAndEvidence(t *testing.T){if evaluate("r",fixtures(127)).Status!="insufficient_n"{t.Fatal("minimum sample")};if evaluate("r",fixtures(128)).Status!="passed_source_supported_country_precision"{t.Fatal("128 all-supported should pass")};a:=fixtures(128);a[0].Evidence="";g:=evaluate("r",a);if g.Status!="failed_precision"||g.Selected!=128||g.Successes!=127{t.Fatal("missing proof removed from denominator",g)}}
func TestSystemicOverridesGoodRateAndUnknown(t *testing.T){a:=fixtures(200);a[0].SystemicError=true;if evaluate("r",a).Status!="blocked_systemic_error"{t.Fatal("systemic error hidden by mean")};a[0].SystemicError=false;a[0].CountrySupported=nil;g:=evaluate("r",a);if g.Selected!=200||g.Successes!=199{t.Fatal("unknown not retained",g)}}
func TestDuplicateWorkRejected(t *testing.T){defer func(){if recover()==nil{t.Fatal("duplicate audit unit accepted")}}();a:=fixtures(128);a[1].ID=a[0].ID;evaluate("r",a)}
