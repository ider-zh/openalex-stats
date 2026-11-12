/* Real-data explorer. All requests stay on this Go service; no runtime CDN. */
(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  const metrics = {
    publications: {name:'论文量', english:'ANNUAL PUBLICATIONS', description:'按发表年份统计；保留当前国家与学科计数权重。'},
    citations: {name:'当年被引次数', english:'ANNUAL RECEIVED CITATIONS', description:'当年实际收到的集合内引用，包含对更早发表文献的引用。'},
    share: {name:'全球学科份额', english:'SHARE OF DISCIPLINE', description:'同年、同学科、已知国家计数中的占比。'},
    rca: {name:'相对比较优势 RCA', english:'REVEALED COMPARATIVE ADVANTAGE', description:'RCA > 1 表示该学科在本国科研结构中相对更突出。'},
    eci: {name:'国家知识复杂度 ECI', english:'COUNTRY KNOWLEDGE COMPLEXITY', description:'使用当前层级全部学科建模；切换单一学科不会改变国家 ECI。'},
    pci: {name:'学科知识复杂度 PCI', english:'GLOBAL DISCIPLINE COMPLEXITY', description:'每个所选学科各有一条全球年度复杂度曲线。'}
  };
  const state = {meta:null, method:'java-reflections-v1', corpus:'core', catalogSupported:false, modelView:'rank', scheme:'topics', level:'field', discipline:'', disciplines:[], flagCodes:new Set(), maxSeries:48, scenario:'', countries:[], metric:'publications', basis:'publications', start:2000, end:2025};
  const palette = ['#bd5938','#315f7b','#4c806d','#ad8a37','#806b91','#738d9b','#a9586c','#5c7354','#9d795d','#49666d','#74637a','#6b819f'];
  const preferredCountries=['CN','US','GB','DE','FR','JP','IN','CA','KR'];
  let chineseRegions,englishRegions;try{chineseRegions=new Intl.DisplayNames(['zh-CN'],{type:'region'});englishRegions=new Intl.DisplayNames(['en'],{type:'region'});}catch{}
  const number = new Intl.NumberFormat('zh-CN', {maximumFractionDigits:2});
  let chart, controller, requestVersion = 0, currentResponse, ordinaryCountries=null, eciCountries=null, expandedCountries=false, readyAnnounced=false;

  const statusName = status => ({ok:'有效', disconnected:'网络断连', degenerate:'模型退化', empty:'样本不足', inactive:'无有效优势', model_not_available:'该场景未计算模型', no_denominator:'分母为零', unstable:'模型接近退化'}[status] || status);
  const escape = value => String(value).replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
  function color(id) {const preferred=preferredCountries.indexOf(id);if(preferred>=0)return palette[preferred];let hash=0;for(const char of id) hash=(hash*31+char.charCodeAt(0))>>>0;return palette[hash%palette.length]; }
  function format(value) { if(value == null) return '—';return state.metric==='share' ? `${number.format(value*100)}%` : number.format(value); }
  function compact(value) {if(Math.abs(value)>=1e8) return `${number.format(value/1e8)}亿`;if(Math.abs(value)>=1e4) return `${number.format(value/1e4)}万`;return number.format(value);}
  function option(select, value, text) { const el=document.createElement('option');el.value=value;el.textContent=text;select.append(el); }
  function choice(parent,key,value,label,selected,onClick){const b=document.createElement('button');b.type='button';b.dataset[key]=value;b.textContent=label;b.setAttribute('aria-pressed',String(selected));b.addEventListener('click',onClick);parent.append(b);return b;}
  function currentLayer() {return state.meta.layers.find(layer=>layer.scheme===state.scheme&&layer.level===state.level);}
  function disciplineName() {return state.disciplines.length===1?(currentLayer()?.labels.find(item=>item.id===state.disciplines[0])?.display_name||state.disciplines[0]):`${state.disciplines.length} 个学科对照`;}
  function currentScenario() {return state.meta.scenarios.find(s=>s.id===state.scenario);}
  function rankView(){return ['eci','pci'].includes(state.metric)&&state.modelView==='rank';}
  function renderScenarios() {
    const main=state.meta.scenarios.find(s=>s.id===state.meta.eci_scenario_id);let selected=currentScenario();
    if(state.scheme==='topics'&&main&&selected&&selected.concept_threshold!==main.concept_threshold){const counterpart=state.meta.scenarios.find(s=>s.country_mode===selected.country_mode&&s.concept_mode===selected.concept_mode&&s.concept_threshold===main.concept_threshold);if(counterpart){selected=counterpart;state.scenario=counterpart.id;}}
    $('scenario').replaceChildren();
    for(const scenario of state.meta.scenarios){
      // Topics ignore Concepts thresholds; expose the matching main-threshold
      // country choices instead of six indistinguishable Topics scenarios.
      if(state.scheme==='topics'&&scenario.concept_threshold!==main?.concept_threshold&&scenario.id!==selected?.id)continue;
      const country=scenario.country_mode==='whole'?'国家全计数':'国家均分';
      const concept=scenario.concept_mode==='whole'?'Concepts 命中全计数':'Concepts 同层均分（旧）';
      option($('scenario'),scenario.id,state.scheme==='topics'?`${country} · Topics 主主题`:`${country} · ${concept} ≥ ${scenario.concept_threshold}`);
    }
    $('scenario').value=state.scenario;
    renderScenarioControls();
  }
  function renderScenarioControls(){
    const selected=currentScenario();if(!selected)return;
    $('countryModeButtons').replaceChildren();for(const mode of ['fractional','whole']){const candidate=state.meta.scenarios.find(s=>s.country_mode===mode&&s.concept_mode===selected.concept_mode&&s.concept_threshold===selected.concept_threshold);if(candidate)choice($('countryModeButtons'),'countryMode',mode,mode==='whole'?'全计数':'均分',selected.country_mode===mode,()=>{state.scenario=candidate.id;renderScenarios();update();});}
    $('thresholdWrap').hidden=state.scheme==='topics';$('thresholdButtons').replaceChildren();
    const scenarios=state.meta.scenarios.filter(s=>s.country_mode===selected.country_mode&&s.concept_mode===selected.concept_mode).sort((a,b)=>a.concept_threshold-b.concept_threshold);
    for(const candidate of scenarios)choice($('thresholdButtons'),'threshold',String(candidate.concept_threshold),`≥ ${candidate.concept_threshold}`,candidate.id===selected.id,()=>{state.scenario=candidate.id;renderScenarios();update();});
  }
  function defaultCountries() {const ids=state.meta.countries.map(c=>c.id);const preferred=preferredCountries.filter(c=>ids.includes(c));return preferred.length?preferred:ids.slice(0,3);}
  function countryChinese(country){try{return chineseRegions?.of(country.id)||country.display_name;}catch{return country.display_name;}}
  function countryEnglish(country){try{return englishRegions?.of(country.id)||country.display_name;}catch{return country.display_name;}}
  function seriesCountry(series){return series.country||series.id;}
  function flagURL(code){return state.flagCodes.has(code)?`/static/flags/${code.toLowerCase()}.svg`:null;}
  function flagHTML(code){const url=flagURL(code);return url?`<img class="country-flag" src="${url}" alt="" aria-hidden="true">`:'<span class="country-neutral" aria-hidden="true">◎</span>';}
  function flagElement(code){const span=document.createElement('span');span.className='country-icon';span.innerHTML=flagHTML(code);return span;}
  function seriesDisplayName(series){const code=seriesCountry(series),country=state.meta.countries.find(c=>c.id===code),name=country?countryChinese(country):(series.country_name||series.name),subject=series.discipline_name||currentLayer()?.labels.find(x=>x.id===series.discipline)?.display_name||disciplineName();return code==='GLOBAL'?`${subject} · 全球 PCI`:state.metric!=='eci'&&state.disciplines.length>1?`${name} · ${subject}`:name;}
  const subjectDashes=['solid','dashed','dotted',[10,3,2,3],[6,3,6,8],[2,2,8,2],[12,5],[3,7]];
  const subjectSymbols=['circle','rect','triangle','diamond','roundRect','pin'];
  function styleFor(series){const i=Math.max(0,state.disciplines.indexOf(series.discipline));return state.metric==='eci'?{dash:'solid',symbol:'circle'}:{dash:subjectDashes[i%subjectDashes.length],symbol:subjectSymbols[Math.floor(i/subjectDashes.length)%subjectSymbols.length]};}
  function seriesColor(id){const series=currentResponse?.series.find(s=>s.id===id),code=series?seriesCountry(series):id;return code==='GLOBAL'?palette[Math.max(0,state.disciplines.indexOf(series?.discipline))%palette.length]:color(code);}
  function legendOptions(data){const rich={},names=new Map();data.forEach((series,i)=>{names.set(series.name,{series,i});const url=flagURL(seriesCountry(series));if(url)rich[`flag${i}`]={width:19,height:14,backgroundColor:{image:url},align:'center'};});return {type:'scroll',top:1,left:0,right:0,itemWidth:20,itemHeight:5,icon:'roundRect',textStyle:{fontSize:13,color:'#536770',rich},tooltip:{show:true,formatter:p=>escape(p.name||'')},formatter:name=>{const entry=names.get(name);if(!entry)return name;const label=name.length>(innerWidth<560?23:40)?name.slice(0,innerWidth<560?22:39)+'…':name;return `${flagURL(seriesCountry(entry.series))?`{flag${entry.i}|} `:''}${label}`;}};}
  function curveCount(){return state.metric==='eci'?state.countries.length:state.metric==='pci'?state.disciplines.length:state.countries.length*state.disciplines.length;}
  function selectionMessage(message){$('selectionNotice').textContent=message;$('selectionNotice').hidden=!message;}
  function allowSelection(countries,disciplines){const n=state.metric==='eci'?countries.length:state.metric==='pci'?disciplines.length:countries.length*disciplines.length;if(state.metric!=='eci'&&(countries.length>12||disciplines.length>state.maxSeries||n>state.maxSeries)){selectionMessage(`当前操作会产生 ${n} 条曲线。数量指标最多 12 个国家、${state.maxSeries} 条曲线，请先减少选择。`);return false;}selectionMessage('');return true;}
  function matchingCountries(){const filter=$('countrySearch').value.trim().toLocaleLowerCase();const exact=state.meta.countries.find(c=>c.id.toLowerCase()===filter);return state.meta.countries.filter(c=>!filter||(exact?c.id===exact.id:`${c.id} ${c.display_name} ${countryChinese(c)} ${countryEnglish(c)}`.toLocaleLowerCase().includes(filter)));}

  async function boot() {
    try {
      const params=new URLSearchParams(location.search);
      const catalogResponse=await fetch('/api/catalog');
      state.catalogSupported=catalogResponse.ok;
      // The fallback supports an older single-directory service during upgrades.
      const catalog=catalogResponse.ok?await catalogResponse.json():{default_corpus:'core',datasets:[{id:'core',display_name:'主数据 · Core'}]};
      const requested=params.get('corpus')||catalog.default_corpus;
      if(!catalog.datasets.some(d=>d.id===requested))throw new Error('所选语料版本未提供。');
      state.corpus=requested;
      $('corpus').replaceChildren();for(const d of catalog.datasets)option($('corpus'),d.id,d.display_name);$('corpus').value=state.corpus;$('corpus').disabled=catalog.datasets.length<2;
      $('corpusButtons').replaceChildren();for(const d of catalog.datasets)choice($('corpusButtons'),'corpus',d.id,d.id==='all'?'含 XPAC 全数据':'Core 主数据',d.id===state.corpus,()=>switchCorpus(d.id));
      const response=await fetch(`/api/meta${catalogResponse.ok?`?corpus=${state.corpus}`:''}`);if(!response.ok)throw new Error('统计元数据读取失败');
      const meta=await response.json();state.meta=meta;state.flagCodes=new Set(meta.flag_codes||[]);state.maxSeries=meta.max_series||48;meta.methods=(meta.methods?.length?meta.methods:[{id:'spectral-v1',display_name:'谱 ECI · 当前方法',country_iterations:0,discipline_iterations:0}]).map(m=>m.id==='java-reflections-v1'?{...m,display_name:'小倪方法论'}:m);state.method=params.get('method')||(meta.methods.some(m=>m.id==='java-reflections-v1')?'java-reflections-v1':meta.eci_default_method||'spectral-v1');if(!meta.methods.some(m=>m.id===state.method))throw new Error('所选知识复杂度方法未提供。');
      $('corpusHint').textContent=(state.corpus==='all'?'当前为全数据：Core + XPAC。':'当前为主数据：Core。')+'两版各自重算论文、集合内引用及 ECI / PCI，不相加。';
      $('workspace').hidden=!meta.ready;$('notReady').hidden=!!meta.ready;
      if(!meta.ready){$('snapshotLabel').textContent='等待真实统计结果';$('notReadyReason').textContent=meta.reason;return;}
      $('snapshotLabel').textContent=meta.snapshot_date ? `快照 ${meta.snapshot_date}` : '快照日期未提供';
      $('selectedWorks').textContent=compact(meta.selected_works || 0);
      $('retainedEdges').textContent=compact(meta.graph?.retained_edges || 0);
      const q=meta.quality||{}, total=q.publication_works||0;
      $('countryCoverage').textContent=total>0?`${number.format(100*(1-(q.missing_country_publications||0)/total))}%`:'—';
      const pilot=meta.status!=='full_snapshot_complete';
      $('runBadge').textContent=(pilot?'真实样本 · 非全球总量':'完整快照统计')+(state.corpus==='all'?' · Core + XPAC':' · Core');$('runBadge').classList.toggle('pilot',pilot);
      $('notice').hidden=!pilot;if(pilot)$('notice').textContent='当前是实际输入子集的统计结果，引用也限定在该子集内部；可验证口径与交互，不代表全球论文总量或国家排名。';
      $('dateRange').textContent=`${meta.year_start}—${meta.year_end} · ${meta.status}`;
      $('footerStatus').textContent=`${meta.snapshot_date||'SNAPSHOT DATE UNKNOWN'} · REAL DATA`;
      $('nameFallback').hidden=meta.dictionary_loaded&&!meta.name_fallbacks;
      state.scheme=['topics','concepts'].includes(params.get('scheme'))?params.get('scheme'):'topics';
      state.level=params.get('level') || (state.scheme==='topics'?'field':'0');
      if(!meta.layers.some(l=>l.scheme===state.scheme&&l.level===state.level))state.level=state.scheme==='topics'?'field':'0';
      state.metric=metrics[params.get('metric')]?params.get('metric'):'publications';
      state.basis=params.get('basis')==='citations'?'citations':'publications';
      state.scenario=meta.scenarios.some(s=>s.id===params.get('scenario'))?params.get('scenario'):(meta.eci_scenario_id||meta.scenarios[0]?.id||'');
      state.start=Math.max(2000,meta.year_start);state.end=Math.max(state.start,Math.min(2025,meta.year_end));
      const from=Number(params.get('start')),to=Number(params.get('end'));
      if(params.has('start')&&from>=state.start&&from<=meta.year_end)state.start=from;
      if(params.has('end')&&to>=state.start&&to<=meta.year_end)state.end=to;
      const known=new Set(meta.countries.map(c=>c.id)),requestedCountries=[...new Set((params.get('countries')||'').split(',').filter(Boolean))];
      if(requestedCountries.some(c=>!known.has(c)))throw new Error('URL 含未知国家／地区代码。');
      state.countries=params.has('countries')?requestedCountries:(state.metric==='eci'?meta.countries.map(c=>c.id):defaultCountries());
      ordinaryCountries=state.metric==='eci'?defaultCountries():[...state.countries];eciCountries=state.metric==='eci'?[...state.countries]:null;
      if(!allowSelection(state.countries,state.disciplines))throw new Error('URL 的国家选择超过数量指标上限，请使用 ECI 或减少选择。');
      renderScenarios();
      renderLayers(params.has('disciplines')?params.get('disciplines').split(',').filter(Boolean):params.has('discipline')?[params.get('discipline')]:undefined);renderYears();renderCountries();renderScope();syncButtons();
      chart=echarts.init($('chart'),null,{renderer:'svg'});window.addEventListener('resize',()=>{if(currentResponse)draw(currentResponse);else chart.resize();});
      new ResizeObserver(()=>chart.resize()).observe($('chart'));
      await update();
    } catch(error) {$('workspace').hidden=true;$('notReady').hidden=false;$('notReadyReason').textContent=error.message;$('snapshotLabel').textContent='结果读取失败';}
  }

  function renderLayers(preferred) {
    $('level').replaceChildren();for(const layer of state.meta.layers.filter(l=>l.scheme===state.scheme)){option($('level'),layer.level,layer.scheme==='topics'?(layer.level==='field'?'Field · 学科':'Subfield · 子学科'):`Level ${layer.level}`);} $('level').value=state.level;
    const labels=currentLayer()?.labels||[];$('discipline').replaceChildren();for(const item of labels)option($('discipline'),item.id,item.display_name);
    const explicit=Array.isArray(preferred),candidates=explicit?preferred:state.disciplines,known=new Set(labels.map(x=>x.id));state.disciplines=[...new Set(candidates.filter(id=>known.has(id)))];if(!explicit&&!state.disciplines.length&&labels.length)state.disciplines=[labels[0].id];state.discipline=state.disciplines[0]||'';$('discipline').value=state.discipline;
    $('levelButtons').replaceChildren();for(const layer of state.meta.layers.filter(l=>l.scheme===state.scheme))choice($('levelButtons'),'level',layer.level,layer.scheme==='topics'?(layer.level==='field'?'Field':'Subfield'):`Level ${layer.level}`,layer.level===state.level,()=>{state.level=layer.level;state.disciplines=[];$('disciplineSearch').value='';renderLayers();update();});
    renderDisciplineTree();
  }
  function toggleDiscipline(id){const next=state.disciplines.includes(id)?state.disciplines.filter(x=>x!==id):[...state.disciplines,id];if(!allowSelection(state.countries,next))return;state.disciplines=next;state.discipline=next[0]||'';$('discipline').value=state.discipline;renderDisciplineTree();update();}
  function renderDisciplineTree(){
    const rootLevel=state.scheme==='topics'?'field':'0',childLevel=state.scheme==='topics'?'subfield':'1';
    const roots=state.meta.layers.find(l=>l.scheme===state.scheme&&l.level===rootLevel)?.labels||[],children=state.meta.layers.find(l=>l.scheme===state.scheme&&l.level===childLevel)?.labels||[];
    const filter=$('disciplineSearch').value.trim().toLocaleLowerCase(),tree=$('disciplineTree');tree.replaceChildren();$('disciplineSummary').textContent=state.metric==='eci'?'ECI 使用当前层全部学科':`已选 ${state.disciplines.length} 个学科 · 点击多选`;$('disciplineCount').textContent=state.disciplines.length;
    $('selectedDisciplines').replaceChildren();for(const id of state.disciplines){const label=currentLayer()?.labels.find(x=>x.id===id),b=document.createElement('button');b.type='button';b.dataset.removeDiscipline=id;b.textContent=`${label?.display_name||id} ×`;b.disabled=state.metric==='eci';b.title=`移除 ${label?.display_name||id}`;b.addEventListener('click',()=>toggleDiscipline(id));$('selectedDisciplines').append(b);}
    const matches=label=>`${label.id} ${label.display_name}`.toLocaleLowerCase().includes(filter);
    const makeNode=(label,level)=>{const b=document.createElement('button');b.type='button';b.className='tree-node';b.dataset.disciplineId=label.id;b.dataset.nodeLevel=level;b.setAttribute('role','treeitem');b.setAttribute('aria-level',level===rootLevel?'1':'2');const checked=state.disciplines.includes(label.id)&&level===state.level;b.setAttribute('aria-selected',String(checked));b.setAttribute('aria-checked',String(checked));const mark=document.createElement('span');mark.className='tree-check';mark.textContent=level===state.level?(checked?'☑':'☐'):'';mark.setAttribute('aria-hidden','true');const text=document.createElement('span');text.textContent=label.display_name;b.append(mark,text);b.title=label.id;b.disabled=level!==state.level||state.metric==='eci';b.addEventListener('click',event=>{event.preventDefault();toggleDiscipline(label.id);});return b;};
    const appendGroup=(parent,items)=>{const visible=filter?(matches(parent)?items:items.filter(matches)):items;if(filter&&!matches(parent)&&!visible.length)return;const group=document.createElement('details');group.className='tree-group';group.dataset.parentId=parent.id;group.open=!!filter||visible.some(c=>state.disciplines.includes(c.id)&&state.level===childLevel);const title=document.createElement('summary');title.append(makeNode(parent,rootLevel));group.append(title);const branch=document.createElement('div');branch.setAttribute('role','group');for(const child of visible)branch.append(makeNode(child,childLevel));group.append(branch);tree.append(group);};
    for(const root of roots){const items=children.filter(c=>c.parent_ids?.includes(root.id));if(items.length)appendGroup(root,items);else if(!filter||matches(root))tree.append(makeNode(root,rootLevel));}
    const rootIDs=new Set(roots.map(r=>r.id)),orphans=children.filter(c=>!c.parent_ids?.some(id=>rootIDs.has(id)));if(orphans.length){const group=document.createElement('div');group.className='tree-orphans';const title=document.createElement('p');title.textContent='父级关系未提供';group.append(title);for(const child of orphans.filter(c=>!filter||matches(c)))group.append(makeNode(child,childLevel));if(group.children.length>1)tree.append(group);}
    $('hierarchyHint').textContent=orphans.length?`${orphans.length} 个子节点缺少可用父级关系，单独列出；不推测层级。`:'父子关系来自快照字典；只可选当前层级节点。';
    if(!tree.children.length){const empty=document.createElement('p');empty.className='hint';empty.textContent='没有匹配的学科。';tree.append(empty);}
    const n=curveCount();$('curveCount').textContent=state.metric==='eci'?`${state.countries.length} 个国家 · ECI 使用全部学科`:state.metric==='pci'?`${state.disciplines.length} 个学科 · ${n} 条全球 PCI 曲线`:`${state.countries.length} 个国家 × ${state.disciplines.length} 个学科 = ${n} 条曲线`;
  }
  function renderYears() {
    for(const id of ['yearStart','yearEnd']){const select=$(id);select.replaceChildren();for(let year=Math.max(2000,state.meta.year_start);year<=state.meta.year_end;year++){option(select,String(year),`${year}${state.meta.partial_years.includes(year)?'（未完整）':''}`);}}
    $('yearStart').value=String(state.start);$('yearEnd').value=String(state.end);
  }
  function renderCountries() {
    const list=$('countryList'),scroll=list.scrollTop,active=list.contains(document.activeElement)?document.activeElement.value:null;list.replaceChildren();
    $('selectedCountries').replaceChildren();const visible=expandedCountries||state.countries.length<=12?state.countries:state.countries.slice(0,6);for(const id of visible){const c=state.meta.countries.find(c=>c.id===id);const b=document.createElement('button');b.type='button';b.dataset.removeCountry=id;b.append(flagElement(id),document.createTextNode(`${countryChinese(c)} ×`));b.disabled=state.metric==='pci';b.title=`移除 ${c.display_name} (${id})`;b.addEventListener('click',()=>{state.countries=state.countries.filter(value=>value!==id);renderCountries();update();});$('selectedCountries').append(b);}
    if(state.countries.length>12){const b=document.createElement('button');b.type='button';b.id='expandCountries';b.textContent=expandedCountries?'收起国家标签':`另 ${state.countries.length-6} 个 · 展开`;b.setAttribute('aria-expanded',String(expandedCountries));b.addEventListener('click',()=>{expandedCountries=!expandedCountries;renderCountries();});$('selectedCountries').append(b);}
    $('allCountries').hidden=state.metric!=='eci';$('countrySelectionHint').textContent=state.metric==='eci'?`ECI 可展示全部 ${state.meta.countries.length} 个国家／地区；搜索和筛选只改变显示，不重排全球名次。`:'默认 9 个常见科研国家，仅供对照。数量指标最多 12 国、48 条曲线；与 ECI 的国家选择独立保存。';
    const countries=matchingCountries().sort((a,b)=>Number(state.countries.includes(b.id))-Number(state.countries.includes(a.id)));
    for(const country of countries){const label=document.createElement('label');label.className='country-option';const input=document.createElement('input');input.type='checkbox';input.value=country.id;input.checked=state.countries.includes(country.id);input.disabled=state.metric==='pci';input.addEventListener('change',()=>{const next=input.checked?[...state.countries,country.id]:state.countries.filter(c=>c!==country.id);if(!allowSelection(next,state.disciplines)){input.checked=false;return;}state.countries=next;renderCountries();update();});const name=document.createElement('span');name.textContent=countryChinese(country);name.title=country.display_name;const code=document.createElement('small');code.textContent=country.id;label.append(input,flagElement(country.id),name,code);list.append(label);}
    $('selectedCount').textContent=String(state.countries.length);
    list.scrollTop=scroll;if(active){const control=[...list.querySelectorAll('input')].find(input=>input.value===active);control?.focus({preventScroll:true});}
  }
  function tooltipSeries(data,index){return data.map((series,order)=>({series,order,value:series.points[index]?.value})).sort((a,b)=>a.value==null?(b.value==null?a.order-b.order:1):b.value==null?-1:b.value-a.value||a.order-b.order).map(row=>row.series);}
  function renderScope() {
    const meta=state.meta,scope=meta.scope||{};
    const corpus=scope.corpus==='all'?'核心＋扩展语料':'核心语料';const years=scope.corpus_year_start?`文献发表年 ≥ ${scope.corpus_year_start}`:'保留 2000 年前的被引文献';
    $('scopeDescription').textContent=`${corpus}；${years}。排除 ${scope.blacklist?.length||0} 类明确黑名单类型。图表的时间窗口不会改变施引文献所属国家或学科的范围。`;
    const q=meta.quality||{},coverage=Array.isArray(meta.coverage)?meta.coverage.find(item=>item.scenario_id===state.scenario&&item.scheme===state.scheme&&item.level===state.level):null;
    let coverageText=`输出区间内缺少国家信息的文献：${number.format(q.missing_country_publications||0)}；相应被引事件：${number.format(q.missing_country_citations||0)}。`;
    if(coverage){const pub=coverage.publication_coverage_valid?`${number.format(coverage.publication_classification_coverage*100)}%`:'未定义',cit=coverage.citation_coverage_valid?`${number.format(coverage.citation_classification_coverage*100)}%`:'未定义';coverageText+=` 当前层级学科覆盖率：文献 ${pub}，被引 ${cit}；未归类文献 ${number.format(coverage.unclassified_publications)}，未归类被引 ${number.format(coverage.unclassified_citations)}。`;}
    else coverageText+=' 本结果未提供分场景分类覆盖率文件。';$('coverageDescription').textContent=coverageText;
    $('fileDownloads').replaceChildren();const modelFiles=new Set(['eci.json','country_eci.parquet','discipline_pci.parquet','model_diagnostics.json']),extra=state.method==='spectral-v1'?[]:['method-manifest.json','method-completion.json','method-audit.json','method-sensitivity.json'];for(const file of [...meta.downloads,...extra]){const a=document.createElement('a'),methodQuery=(modelFiles.has(file)||extra.includes(file))?`&method=${encodeURIComponent(state.method)}`:'';a.href=`/download?file=${encodeURIComponent(file)}${methodQuery}${state.catalogSupported?`&corpus=${state.corpus}`:''}`;a.textContent=`↓ ${file}`;$('fileDownloads').append(a);}
  }
  function syncButtons() {
    document.querySelectorAll('[data-scheme]').forEach(button=>button.setAttribute('aria-pressed',String(button.dataset.scheme===state.scheme)));
    document.querySelectorAll('[data-metric]').forEach(button=>button.setAttribute('aria-pressed',String(button.dataset.metric===state.metric)));
    renderMethodControls();$('basisWrap').hidden=['publications','citations'].includes(state.metric);$('basis').value=state.basis;
    $('modelViewWrap').hidden=!['eci','pci'].includes(state.metric);document.querySelectorAll('[data-model-view]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.modelView===state.modelView)));
    $('discipline').disabled=state.metric==='eci';
    $('disciplinePicker').classList.toggle('disabled',state.metric==='eci');for(const id of ['clearDisciplines','resetDisciplines','selectDisciplineResults'])$(id).disabled=state.metric==='eci';if(state.metric==='eci')$('disciplinePicker').open=false;
    renderDisciplineTree();
    document.querySelectorAll('[data-basis]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.basis===state.basis)));
    renderScenarioControls();
    const scenario=currentScenario(),country=scenario?.country_mode==='whole'?'国家全计数：每个归属国记 1。':'国家均分：各归属国分得 1/N，论文量仍可为小数。';
    const labels=state.scheme==='topics'?'Topics 按主主题归类；Concepts 阈值不适用。':scenario?.concept_mode==='whole'?'Concepts 同层标签去重后，命中一个标签即记 1，不再均分。'+(scenario?.country_mode==='whole'?'当前论文量为整数计数。':''):'Concepts 同层命中标签均分；这是旧版数据口径。';
    $('scenarioHint').textContent=country+labels+(state.scenario===state.meta.eci_scenario_id?'该场景提供 ECI / PCI 主模型。':'该场景用于口径对照；ECI / PCI 仅在已计算模型时展示。');
    renderCountries();
  }
  function renderMethodControls(){
    const active=['eci','pci'].includes(state.metric),method=state.meta.methods.find(m=>m.id===state.method);$('methodWrap').hidden=!active;$('methodButtons').replaceChildren();
    for(const item of state.meta.methods){const button=choice($('methodButtons'),'method',item.id,item.id==='spectral-v1'?'谱方法':'小倪方法论',item.id===state.method,()=>{state.method=item.id;renderMethodControls();update();});button.title=item.display_name;button.disabled=state.meta.methods.length<2;}
    $('methodHint').textContent=method?.id==='java-reflections-v1'?`小倪方法论采用有限反射：国家 ${method.country_iterations} 轮、学科 ${method.discipline_iterations} 轮，总体 z 标准化。两方法分别定向，排名可能显著不同，请结合轮数敏感性解读。`:'谱方法取第二特征向量，并作总体 z 标准化。方法选择只影响 ECI / PCI，论文量、被引、份额和 RCA 的口径保持一致。';
    $('methodSensitivity').hidden=method?.id!=='java-reflections-v1';$('methodSensitivity').href=`/download?file=method-sensitivity.json&method=${encodeURIComponent(state.method)}${state.catalogSupported?`&corpus=${state.corpus}`:''}`;
  }
  function queryParams() {const p=new URLSearchParams({method:state.method,scenario:state.scenario,scheme:state.scheme,level:state.level,countries:state.countries.join(','),metric:state.metric,basis:state.basis,start:state.start,end:state.end});if(state.disciplines.length===1)p.set('discipline',state.disciplines[0]);else p.set('disciplines',state.disciplines.join(','));if(state.catalogSupported)p.set('corpus',state.corpus);return p;}
  async function update() {
    if(state.metric==='eci')eciCountries=[...state.countries];else ordinaryCountries=[...state.countries];
    if(!chart)return;if(controller)controller.abort();const version=++requestVersion;syncButtons();renderScope();const metric=metrics[state.metric];$('chartKicker').textContent=metric.english;$('chartTitle').textContent=state.metric==='eci'?`${state.scheme==='topics'?'Topics':'Concepts'} / ${$('level').selectedOptions[0]?.textContent} · 国家整体` : disciplineName();$('metricDescription').textContent=metric.description;
    $('chartError').hidden=true;const params=queryParams();history.replaceState(null,'',`/?${params}`);$('downloadSeries').href=`/api/series.csv?${params}`;
    if((!state.countries.length&&state.metric!=='pci')||(!state.disciplines.length&&state.metric!=='eci')){chart.hideLoading();chart.clear();$('chartError').hidden=false;$('chartError').textContent='请至少选择一个国家／地区及有效学科。';$('comparisonBody').replaceChildren();return;}
    if(state.metric!=='eci'&&(state.countries.length>12||curveCount()>state.maxSeries)){chart.hideLoading();chart.clear();$('chartError').hidden=false;$('chartError').textContent=`数量指标最多 12 国、${state.maxSeries} 条曲线，请减少国家或学科。`;$('comparisonBody').replaceChildren();return;}controller=new AbortController();chart.showLoading('default',{text:'查询真实统计结果…',color:'#c6613c',maskColor:'rgba(255,254,250,.85)',fontSize: 14});
    try {const response=await fetch(`/api/series?${params}`,{signal:controller.signal});const body=await response.json();if(!response.ok)throw new Error(body.error||'统计查询失败');if(version!==requestVersion)return;currentResponse=body;draw(body);}
    catch(error){if(error.name==='AbortError')return;chart.clear();$('chartError').hidden=false;$('chartError').textContent=error.message;}
    finally{if(version===requestVersion)chart.hideLoading();}
  }
  function draw(response) {
    const displayed={...response,series:response.series.map(series=>({...series,name:seriesDisplayName(series)}))};
    const tall=state.metric==='eci'&&rankView(),height=tall?window.OpenAlexBump.layout(displayed,{mobile:innerWidth<560}).height:null;$('chart').style.height=height?`${height}px`:'';$('chart').classList.toggle('global-bump',tall);chart.resize();
    $('bumpScopeNote').hidden=!tall;$('bumpScopeNote').textContent=tall?`展示 ${displayed.series.length} 个国家／地区；图高按完整模型名次展开。向下滚动可查看全部排名，悬停曲线可高亮定位；无效国家保留在搜索、提示框和终年表。`:'';
    const percent=state.metric==='share',data=displayed.series,years=data[0]?.points.map(point=>`${point.year}${point.partial?'*':''}`)||[];
    // Quantitative lines and the table commit together; interpolating old curves would temporarily show different values.
    if(rankView()){chart.setOption(window.OpenAlexBump.buildOption(displayed,{tooltipPosition:(point,params,dom,rect,size)=>{const b=$('chart').getBoundingClientRect(),h=size.contentSize[1],w=size.contentSize[0],minY=Math.max(0,12-b.top),maxY=Math.min(b.height-h,innerHeight-12-b.top-h);return [Math.max(0,Math.min(point[0]+16,b.width-w)),Math.max(minY,Math.min(point[1]+16,maxY))];},color:seriesColor,format:value=>number.format(value),mobile:innerWidth<560,flagURL:id=>flagURL(seriesCountry(data.find(s=>s.id===id)||{id})),legend:legendOptions(data),style:id=>styleFor(data.find(s=>s.id===id)||{id}),endLabel:series=>seriesCountry(series)==='GLOBAL'?(series.discipline_name||'PCI').slice(0,12):seriesCountry(series)}),true);}else chart.setOption({animation:false,animationDuration:0,animationDurationUpdate:0,color:data.map(series=>seriesColor(series.id)),textStyle:{fontFamily:'Trebuchet MS, Microsoft YaHei, sans-serif',fontSize: 13,color:'#718087'},grid:{left:12,right:16,top:62,bottom:24,containLabel:true},legend:legendOptions(data),tooltip:{trigger:'axis',order:'valueDesc',confine:true,backgroundColor:'#fffefa',borderColor:'#d7dcd8',textStyle:{fontSize: 14,color:'#162d39'},enterable:true,formatter:items=>{const index=items[0]?.dataIndex;if(index===undefined)return '';let html=`<div style="font-weight:700;margin-bottom:7px">${escape(years[index])}${years[index].endsWith('*')?' · 未完整年度':''}</div><div style="font-size:12px;color:#61757c;margin-bottom:7px">共 ${data.length} 条曲线 · 当年值降序${data.length>8?" · 可滚动查看":""}</div><div class="tooltip-list">`;for(const series of tooltipSeries(data,index)){const point=series.points[index];if(!point)continue;html+=`<div data-tooltip-series="${escape(series.id)}" data-tooltip-value="${point.value==null?'':point.value}" data-tooltip-year="${point.year}" class="tooltip-row"><span class="tooltip-label" style="color:${seriesColor(series.id)}">${flagHTML(seriesCountry(series))}<span>● ${escape(series.name)}</span></span><strong>${point.value==null?escape(statusName(point.status)):escape(format(point.value))}</strong></div>`;}return html+'</div>';}},xAxis:{type:'category',boundaryGap:false,data:years,axisLine:{lineStyle:{color:'#cbd2d0'}},axisTick:{show:false},axisLabel:{color:'#61757c',fontSize: 12,hideOverlap:true,margin:13}},yAxis:{type:'value',scale:state.metric==='eci'||state.metric==='pci',splitNumber:4,axisLabel:{color:'#61757c',fontSize: 12,formatter:value=>percent?`${number.format(value)}%`:compact(value)},splitLine:{lineStyle:{color:'#e7eae4',type:'dashed'}},axisLine:{show:false},axisTick:{show:false}},series:data.map((series,index)=>({id:series.id,name:series.name,type:'line',data:series.points.map(point=>point.value==null?null:(percent?point.value*100:point.value)),connectNulls:false,smooth:false,showSymbol:true,showAllSymbol:true,symbol:styleFor(series).symbol,symbolSize:4,lineStyle:{width:2.4,color:seriesColor(series.id),type:styleFor(series).dash},itemStyle:{color:seriesColor(series.id)},emphasis:{focus:'series',scale:true,lineStyle:{width:3.5}},markLine:index===0&&['rca','eci','pci'].includes(state.metric)?{silent:true,symbol:'none',label:{show:false},lineStyle:{type:'dashed',width:1,color:'#aab5ae'},data:[{yAxis:state.metric==='rca'?1:0}]}:undefined}))},true);
    chart.getZr().flush();
    if(!readyAnnounced){readyAnnounced=true;document.documentElement.dataset.openalexReady='true';$('workspace').dataset.ready='true';document.dispatchEvent(new CustomEvent('openalex:ready',{detail:{corpus:state.corpus,metric:state.metric}}));}
    $('chartDefinition').textContent=rankView()?`${response.definition} ${window.OpenAlexBump.definition(response)}`:response.definition;$('partialBadge').hidden=!data.some(series=>series.points.some(point=>point.partial));
    const gaps=data.flatMap(series=>series.points).filter(point=>point.value==null),unstable=data.flatMap(series=>series.points).filter(point=>point.status==='unstable');
    $('gapNote').hidden=!gaps.length&&!unstable.length;
    if(gaps.length){const reasons=[...new Set(gaps.map(point=>statusName(point.status)))];$('gapNote').textContent=`有 ${gaps.length} 个曲线／年份点无法定义（${reasons.join('、')}），图中保留缺口，不以 0 替代。`;}
    else if(unstable.length)$('gapNote').textContent='部分模型接近退化，分数对微小数据变化可能敏感，请结合模型诊断解读。';
    $('readingNote').textContent=state.metric==='eci'?'国家 ECI 来自该层级的全部学科。学科选择在此指标下被停用，曲线显示国家整体结构的相对位置。':state.metric==='pci'?'PCI 是所选学科在全球生产网络中的复杂度，同一年各国共享这个学科值，因此每个所选学科各绘制一条全球曲线。':state.metric==='rca'?'每条曲线对应一个国家与学科组合。RCA 超过 1，表示该学科在本国科研结构中的相对比重高于全球。':state.metric==='share'?'每条曲线表示该国在所选学科中的年度份额。分母使用已知国家的归属计数，全计数场景不会等同去重的全球论文数。':'每条曲线对应一个国家与学科组合。改变国家选择只改变对照对象，不会把引用来源限定成这些国家。';
    if(rankView())$('readingNote').textContent=(state.metric==='eci'?'国家 ECI 来自当前层全部学科结构，不随所选学科重算。':'PCI 是所选学科的全球值。')+'名次按该年完整模型的全部有效'+(state.metric==='pci'?'学科':'国家')+'确定。选择仅改变显示的曲线，不重排全局名次；并列同名次，后续跳号。每年有效总数见提示框和终年表。';
    $('tableYear').textContent=String(state.end);$('tableMetric').textContent=rankView()?'全球排名':metrics[state.metric].name;$('tableScore').hidden=!rankView();$('tableEntity').textContent=state.metric==='pci'?'全球学科':state.metric==='eci'?'国家／地区':'国家 · 学科';$('comparisonBody').replaceChildren();
    for(const series of [...data].sort((a,b)=>(b.points.at(-1)?.value??-Infinity)-(a.points.at(-1)?.value??-Infinity))){const point=series.points.at(-1);const tr=document.createElement('tr'),name=document.createElement('td'),dot=document.createElement('span');dot.className='series-color';dot.style.background=seriesColor(series.id);name.append(flagElement(seriesCountry(series)),dot,document.createTextNode(series.name));tr.dataset.seriesId=series.id;tr.dataset.year=String(point?.year??'');tr.dataset.value=point?.value==null?'':String(point.value);tr.dataset.status=point?.status??'missing';tr.dataset.rank=point?.rank==null?'':String(point.rank);const value=document.createElement('td');value.textContent=rankView()?(point?.rank==null?'—':`#${point.rank} / ${point.rank_universe}`):format(point?.value);const score=document.createElement('td');score.className='score-column';score.hidden=!rankView();score.textContent=format(point?.value);const status=document.createElement('td');status.textContent=point?`${statusName(point.status)}${point.partial?' · 未完整':''}`:'无数据';tr.append(name,value,score,status);$('comparisonBody').append(tr);}
  }

  document.querySelectorAll('[data-scheme]').forEach(button=>button.addEventListener('click',()=>{if(!state.meta?.ready||state.scheme===button.dataset.scheme)return;state.scheme=button.dataset.scheme;state.level=state.scheme==='topics'?'field':'0';state.discipline='';state.disciplines=[];renderLayers();renderScenarios();update();}));
  document.querySelectorAll('[data-metric]').forEach(button=>button.addEventListener('click',()=>{const next=button.dataset.metric;if(next===state.metric)return;if(state.metric==='eci')eciCountries=[...state.countries];else ordinaryCountries=[...state.countries];state.metric=next;state.countries=next==='eci'?[...(eciCountries??state.meta.countries.map(c=>c.id))]:[...(ordinaryCountries??defaultCountries())];expandedCountries=false;selectionMessage('');update();}));
  $('level').addEventListener('change',()=>{state.level=$('level').value;state.discipline='';state.disciplines=[];renderLayers();update();});$('discipline').addEventListener('change',()=>{state.discipline=$('discipline').value;state.disciplines=state.discipline?[state.discipline]:[];update();});$('scenario').addEventListener('change',()=>{state.scenario=$('scenario').value;update();});$('basis').addEventListener('change',()=>{state.basis=$('basis').value;update();});
  $('disciplineSearch').addEventListener('input',renderDisciplineTree);
  $('clearDisciplines').addEventListener('click',()=>{selectionMessage('');state.disciplines=[];state.discipline='';$('discipline').value='';renderDisciplineTree();update();});
  $('resetDisciplines').addEventListener('click',()=>{renderLayers([currentLayer()?.labels[0]?.id].filter(Boolean));selectionMessage('');update();});
  $('selectDisciplineResults').addEventListener('click',()=>{const filter=$('disciplineSearch').value.trim().toLocaleLowerCase(),next=[...new Set([...state.disciplines,...currentLayer().labels.filter(l=>`${l.id} ${l.display_name}`.toLocaleLowerCase().includes(filter)).map(l=>l.id)])];if(!allowSelection(state.countries,next))return;renderLayers(next);update();});

  $('disciplineSummary').addEventListener('click',event=>{if(state.metric==='eci')event.preventDefault();});
  document.querySelectorAll('[data-basis]').forEach(button=>button.addEventListener('click',()=>{state.basis=button.dataset.basis;update();}));
  document.querySelectorAll('[data-model-view]').forEach(button=>button.addEventListener('click',()=>{state.modelView=button.dataset.modelView;syncButtons();if(currentResponse)draw(currentResponse);}));
  $('selectCountryResults').addEventListener('click',()=>{const next=[...new Set([...state.countries,...matchingCountries().map(c=>c.id)])];if(!allowSelection(next,state.disciplines))return;state.countries=next;renderCountries();update();});
  $('clearCountries').addEventListener('click',()=>{selectionMessage('');state.countries=[];renderCountries();update();});
  $('allCountries').addEventListener('click',()=>{state.countries=state.meta.countries.map(c=>c.id);expandedCountries=false;selectionMessage('');renderCountries();update();});
  $('yearStart').addEventListener('change',()=>{state.start=Number($('yearStart').value);if(state.end<state.start){state.end=state.start;$('yearEnd').value=String(state.end);}update();});$('yearEnd').addEventListener('change',()=>{state.end=Number($('yearEnd').value);if(state.start>state.end){state.start=state.end;$('yearStart').value=String(state.start);}update();});$('countrySearch').addEventListener('input',renderCountries);$('resetCountries').addEventListener('click',()=>{const next=defaultCountries();if(!allowSelection(next,state.disciplines))return;state.countries=next;renderCountries();update();});$('retry').addEventListener('click',()=>location.reload());
  function switchCorpus(corpus){if(corpus===state.corpus)return;const params=queryParams();params.set('corpus',corpus);location.assign(`/?${params}`);}
  $('corpus').addEventListener('change',()=>switchCorpus($('corpus').value));
  boot();
})();
