package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/results"
)

// Opt-in local browser acceptance uses explicitly hand-sized fixture datasets,
// not a partial global result or an unvalidated production all corpus.
func TestLocalCatalogBrowserControls(t *testing.T) {
	if os.Getenv("OA_WEB_BROWSER_QA") == "" {
		t.Skip("set OA_WEB_BROWSER_QA=1 for installed Chrome/Playwright browser acceptance")
	}
	core, all := catalogFixture(t)
	for _, dir := range []string{core, all} {
		var dictionary Dictionary
		b, err := os.ReadFile(filepath.Join(dir, "dictionary.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(b, &dictionary); err != nil {
			t.Fatal(err)
		}
		for _, c := range []Label{{ID: "DE", DisplayName: "Germany"}, {ID: "FR", DisplayName: "France"}, {ID: "JP", DisplayName: "Japan"}, {ID: "IN", DisplayName: "India"}, {ID: "CA", DisplayName: "Canada"}, {ID: "KR", DisplayName: "South Korea"}, {ID: "AU", DisplayName: "Australia"}} {
			dictionary.Countries = append(dictionary.Countries, c)
		}
		jsonFile(t, dir, "dictionary.json", dictionary)
		cube, err := parquet.ReadFile[aggregate.Row](filepath.Join(dir, "cube.parquet"))
		if err != nil {
			t.Fatal(err)
		}
		var rows []results.Row
		var scenarios []results.Scenario
		for _, countryMode := range []string{"fractional", "whole"} {
			for _, threshold := range []struct {
				value float64
				text  string
			}{{0, "0.0"}, {.3, "0.3"}, {.5, "0.5"}} {
				id := countryMode + "-concept-whole-" + threshold.text
				scenario := results.Scenario{ID: id, CountryMode: countryMode, ConceptMode: "whole", ConceptThreshold: threshold.value, Main: threshold.value == .3}
				scenarios = append(scenarios, scenario)
				for _, r := range cube {
					rows = append(rows, results.Row{ScenarioID: id, CountryMode: countryMode, ConceptMode: "whole", ConceptThreshold: threshold.value, Main: scenario.Main, Country: r.Country, Year: r.Year, Scheme: r.Scheme, Level: r.Level, DisciplineID: r.DisciplineID, Publications: r.Publications, Citations: r.Citations})
				}
			}
		}
		parquetFixture(t, filepath.Join(dir, "statistics.parquet"), rows)
		jsonFile(t, dir, "scenarios.json", scenarios)
	}
	handler, err := NewCatalog(map[string]string{"core": core, "all": all})
	if err != nil {
		t.Fatal(err)
	}
	service := httptest.NewServer(handler)
	defer service.Close()
	output := os.Getenv("OA_WEB_BROWSER_OUTPUT")
	if output == "" {
		output = t.TempDir()
	}
	output, err = filepath.Abs(output)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", "-e", localBrowserAssertions, service.URL, output)
	data, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("local browser controls: %v\n%s", err, data)
	}
	t.Log(string(data))
}

const localBrowserAssertions = `
const {chromium}=require('../../bin/browser-qa/node_modules/playwright');const assert=require('assert/strict');const fs=require('fs/promises');const path=require('path');
(async()=>{const base=process.argv[1],out=process.argv[2];await fs.mkdir(out,{recursive:true});const browser=await chromium.launch({channel:'chrome',headless:true});try{
const page=await browser.newPage({viewport:{width:1440,height:1100}});await page.addInitScript(()=>localStorage.setItem('openalex-atlas:onboarding:v1',JSON.stringify({version:1,dismissedAt:Date.now()})));const errors=[];page.on('pageerror',e=>errors.push(e.message));
const body=()=>page.evaluate(async()=> (await fetch('/api/series'+location.search)).json());const wait=async()=>{await page.waitForFunction(()=>document.querySelector('#chart svg, #chart canvas')&&document.querySelector('#chartError').hidden&&document.querySelector('#chartDefinition').textContent.length>0);};
const act=async(fn,predicate)=>{const pending=page.waitForResponse(r=>r.url().includes('/api/series?')&&r.status()===200&&predicate(new URL(r.url()).searchParams));pending.catch(()=>{});await fn();const data=await(await pending).json();await page.waitForFunction(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).getZr().animation.isFinished());return data;};
await page.goto(base,{waitUntil:'networkidle'});await wait();assert.equal(await page.locator('#corpusButtons button').count(),2);assert.equal((await body()).query.countries.length,9);
let option=await page.evaluate(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).getOption());assert.ok(option.series.every(s=>s.showSymbol));assert.equal(new Set(option.color).size,9);
for(const term of ['中国','China','CN']){await page.locator('#countrySearch').fill(term);assert.equal(await page.locator('#countryList input[value="CN"]').count(),1);}
await page.locator('#countrySearch').fill('Australia');await act(()=>page.locator('#selectCountryResults').click(),p=>p.get('countries').split(',').includes('AU'));
await page.locator('#clearCountries').click();await page.locator('#chartError').waitFor({state:'visible'});await act(()=>page.locator('#resetCountries').click(),p=>p.get('countries').split(',').length===9);await page.locator('#countrySearch').fill('');
await act(()=>page.locator('[data-level="subfield"]').click(),p=>p.get('level')==='subfield');await page.locator('#disciplineSummary').click();await page.locator('#clearDisciplines').click();await page.locator('#disciplineSearch').fill('细胞');assert.equal(await page.locator('[data-parent-id="f1"] [data-discipline-id="s1"]').count(),1);await act(()=>page.locator('[data-discipline-id="s1"]').click(),p=>p.get('discipline')==='s1');await page.locator('#disciplineSummary').click();
await act(()=>page.locator('[data-scheme="concepts"]').click(),p=>p.get('scheme')==='concepts');assert.equal(await page.locator('#thresholdWrap').isVisible(),true);await act(()=>page.locator('[data-threshold="0.5"]').click(),p=>p.get('scenario')==='fractional-concept-whole-0.5');await act(()=>page.locator('[data-country-mode="whole"]').click(),p=>p.get('scenario')==='whole-concept-whole-0.5');await act(()=>page.locator('[data-country-mode="fractional"]').click(),p=>p.get('scenario')==='fractional-concept-whole-0.5');await act(()=>page.locator('[data-threshold="0.3"]').click(),p=>p.get('scenario')==='fractional-concept-whole-0.3');await act(()=>page.locator('[data-threshold="0.5"]').click(),p=>p.get('scenario')==='fractional-concept-whole-0.5');
await act(()=>page.locator('[data-scheme="topics"]').click(),p=>p.get('scheme')==='topics');assert.equal(await page.locator('#thresholdWrap').isVisible(),false);assert.equal(await page.locator('#scenario').inputValue(),'fractional-concept-whole-0.3');await page.locator('#disciplineSummary').click();await page.locator('#clearDisciplines').click();await page.locator('#disciplineSearch').fill('f1');await act(()=>page.locator('[data-discipline-id="f1"]').click(),p=>p.get('discipline')==='f1');await page.locator('#disciplineSummary').click();await act(()=>page.locator('[data-metric="eci"]').click(),p=>p.get('metric')==='eci');await act(()=>page.locator('#yearEnd').selectOption('2000'),p=>p.get('end')==='2000');
option=await page.evaluate(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).getOption());assert.equal(option.yAxis[0].inverse,true);assert.equal(option.series[0].data[0],1);assert.equal(option.series[1].data[0],2);
await page.evaluate(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).dispatchAction({type:'showTip',seriesIndex:0,dataIndex:0}));await page.waitForFunction(()=>document.querySelectorAll('[data-tooltip-series]').length===9);const tooltip=await page.locator('[data-tooltip-series]').evaluateAll(rows=>rows.map(r=>({id:r.dataset.tooltipSeries,rank:r.dataset.tooltipRank})));assert.deepEqual(tooltip.slice(0,2).map(x=>x.id),['CN','US']);assert.ok(tooltip.slice(2).every(x=>x.rank===''));await page.evaluate(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).dispatchAction({type:'hideTip'}));
await page.locator('[data-model-view="score"]').click();option=await page.evaluate(()=>window.echarts.getInstanceByDom(document.querySelector('#chart')).getOption());assert.equal(option.series[0].data[0],1.2);await page.locator('[data-model-view="rank"]').click();await page.screenshot({path:path.join(out,'fixture-core-bump.png'),fullPage:true,animations:'disabled'});
await page.locator('[data-corpus="all"]').click();await page.waitForURL('**corpus=all**');await page.waitForLoadState('networkidle');await wait();const preserved=await body();assert.equal(preserved.query.corpus,'all');assert.equal(preserved.query.end,2000);assert.equal(preserved.query.metric,'eci');assert.equal(preserved.query.countries.length,9);
const allPub=await act(()=>page.locator('[data-metric="publications"]').click(),p=>p.get('metric')==='publications');assert.equal(allPub.series[0].points[0].value,6);assert.ok((await page.locator('#scopeDescription').textContent()).includes('核心＋扩展'));
const downloads=await page.evaluate(async()=>({run:await(await fetch('/download?file=run.json&corpus=all')).json(),csv:await(await fetch(document.querySelector('#downloadSeries').getAttribute('href'))).text()}));assert.equal(downloads.run.selected_works,60);assert.ok(downloads.csv.includes(',6,ok,false,all,'));
await page.setViewportSize({width:390,height:844});await page.waitForFunction(()=>document.documentElement.scrollWidth<=innerWidth+1);await page.screenshot({path:path.join(out,'fixture-all-mobile.png'),fullPage:true,animations:'disabled'});
await page.locator('[data-corpus="core"]').click();await page.waitForURL('**corpus=core**');await page.waitForLoadState('networkidle');await wait();assert.equal((await body()).series[0].points[0].value,2);assert.deepEqual(errors,[]);const report={status:'passed',fixture:true,checks:['catalog core/all isolation and preserved filters','nine defaults unique colors','Chinese English ISO search plus bulk clear reset','real parent tree','one-click country mode and thresholds','rank score switch and global ranks','tooltip null-last','corpus-specific CSV/download','mobile no overflow'],output:out};await fs.writeFile(path.join(out,'fixture-report.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report));
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1});
`
