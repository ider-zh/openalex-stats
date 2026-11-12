// ECharts option builder. Rankings come exclusively from the complete annual
// model on the server; selecting countries only changes which lines are drawn.
(function (root) {
  'use strict';

  const ink = '#162d39', muted = '#718087', paper = '#fffefa';
  const escape = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const finite = value => typeof value === 'number' && Number.isFinite(value);
  const positiveInteger = value => Number.isInteger(value) && value >= 1;
  const seriesOf = response => Array.isArray(response?.series) ? response.series : [];
  const pointsOf = series => Array.isArray(series?.points) ? series.points : [];
  const formatDefault = value => new Intl.NumberFormat('zh-CN', {maximumFractionDigits: 3}).format(value);
  const safeColor = value => /^#[\da-f]{3,8}$/i.test(value || '') ? value : '#315f7b';
  const colorOf = (options, id) => safeColor(options.color?.(id));
  const nameOf = (series, options) => String(options.displayName?.(series) ?? series.name ?? series.id ?? '');

  function years(response) {
    const {start, end} = response?.query || {};
    if (Number.isInteger(start) && Number.isInteger(end) && end >= start && end - start < 500) {
      return Array.from({length: end - start + 1}, (_, i) => start + i);
    }
    return [...new Set(seriesOf(response).flatMap(s => pointsOf(s).map(p => p.year)).filter(Number.isInteger))].sort((a, b) => a - b);
  }

  function validRank(point) {
    // Missing legacy ranks must stay missing. Never derive a rank from the
    // currently displayed scores, even if all selected scores are available.
    if (!point || !finite(point.value) || !positiveInteger(point.rank)) return null;
    if (point.status && !['ok', 'unstable'].includes(point.status)) return null;
    if (positiveInteger(point.rank_universe) && point.rank > point.rank_universe) return null;
    return point.rank;
  }

  function pointMaps(response) {
    return seriesOf(response).map(series => new Map(pointsOf(series).map(point => [point.year, point])));
  }

  function rowsForYear(response, year, maps, options) {
    return seriesOf(response).map((series, i) => {
      const point = maps[i].get(year);
      return {
        id: String(series.id ?? ''), name: nameOf(series, options), year,
        rank: validRank(point), value: finite(point?.value) ? point.value : null,
        rankUniverse: positiveInteger(point?.rank_universe) ? point.rank_universe : null,
        partial: Boolean(point?.partial), status: point?.status || 'missing', order: i
      };
    }).sort((a, b) => (a.rank ?? Infinity) - (b.rank ?? Infinity) ||
      (b.value ?? -Infinity) - (a.value ?? -Infinity) || a.order - b.order);
  }

  function tooltipRows(response, index, options = {}) {
    const year = years(response)[index];
    return year === undefined ? [] : rowsForYear(response, year, pointMaps(response), options);
  }

  function definition(response) {
    const universe = response?.query?.metric === 'pci' ? '同层全部有效学科' : '全部有效国家';
    return `每年按${universe}的复杂度分数降序排名；并列同名次，后续名次跳号。只展示所选曲线，选择不改变全局排名。名次反映相对结构，不能解释为知识总量；原始分数见悬浮提示。`;
  }

  function buildOption(response, options = {}) {
    const axisYears = years(response), data = seriesOf(response), maps = pointMaps(response);
    const format = options.format || options.formatScore || formatDefault;
    const mobile = Boolean(options.mobile);
    const partialYears = new Set(data.flatMap(s => pointsOf(s).filter(p => p.partial).map(p => p.year)));
    const ranks = data.map((s, i) => axisYears.map(year => validRank(maps[i].get(year))));
    const visibleRanks = ranks.flat().filter(positiveInteger);
    const largest = visibleRanks.length ? Math.max(...visibleRanks) : 1;
    const universes = data.flatMap(s => pointsOf(s).map(p => p.rank_universe)).filter(positiveInteger);
    const maxUniverse = universes.length ? Math.max(...universes) : Infinity;
    const axisMax = Math.max(2, Math.min(maxUniverse, Math.max(5, largest + Math.max(1, Math.ceil(largest * .08)))));
    const axisLabels = axisYears.map(year => `${year}${partialYears.has(year) ? '*' : ''}`);

    function tooltip(items) {
      const index = Array.isArray(items) ? items[0]?.dataIndex : items?.dataIndex;
      if (!Number.isInteger(index) || axisYears[index] === undefined) return '';
      const year = axisYears[index], rows = rowsForYear(response, year, maps, options);
      let html = `<div style="font-weight:700;margin-bottom:8px">${year}${partialYears.has(year) ? '* · 未完整年度' : ''}</div>`;
      html += '<div style="font-size:13px;color:#718087;margin-bottom:8px">全局名次 / 有效总数 · 原始分数</div>';
      for (const row of rows) {
        const rank = row.rank === null ? '无有效排名' : `第 ${row.rank} / ${row.rankUniverse ?? '—'}`;
        const score = row.value === null ? '—' : format(row.value);
        html += `<div data-tooltip-series="${escape(row.id)}" data-tooltip-value="${row.value ?? ''}" data-tooltip-year="${year}" data-tooltip-rank="${row.rank ?? ''}" data-tooltip-universe="${row.rankUniverse ?? ''}" style="display:flex;align-items:baseline;justify-content:space-between;gap:18px;margin:6px 0">`;
        html += `<span style="color:${colorOf(options, row.id)}">● ${escape(row.name)}</span>`;
        html += `<span style="text-align:right"><strong>${escape(rank)}</strong><span style="display:inline-block;min-width:65px;margin-left:12px;color:#536770">${escape(score)}</span></span></div>`;
      }
      return html;
    }

    return {
      animation: false, animationDuration: 0, animationDurationUpdate: 0,
      color: data.map(s => colorOf(options, s.id)),
      textStyle: {fontFamily: 'Trebuchet MS, Microsoft YaHei, sans-serif', fontSize: 13, color: muted},
      aria: {enabled: true, label: {description: definition(response)}},
      grid: {left: 14, right: mobile ? 58 : 90, top: 70, bottom: 24, containLabel: true},
      legend: {type: 'scroll', top: 1, left: 0, right: 0, itemWidth: 14, itemHeight: 3, icon: 'roundRect', textStyle: {fontSize: 13, color: '#536770'}},
      tooltip: {trigger: 'axis', confine: true, backgroundColor: paper, borderColor: '#d7dcd8', textStyle: {fontSize: 14, color: ink}, axisPointer: {type: 'line', lineStyle: {color: '#aab5ae', type: 'dashed'}}, formatter: tooltip},
      xAxis: {type: 'category', boundaryGap: false, data: axisLabels, axisLine: {lineStyle: {color: '#cbd2d0'}}, axisTick: {show: false}, axisLabel: {color: '#61757c', fontSize: 12, hideOverlap: true, margin: 13}},
      yAxis: {type: 'value', inverse: true, min: 1, max: axisMax, minInterval: 1, splitNumber: Math.min(6, axisMax - 1), name: '全局排名 ↑', nameLocation: 'start', nameGap: 26, nameTextStyle: {fontSize: 13, color: muted, align: 'left'}, axisLabel: {color: '#61757c', fontSize: 12, formatter: value => Number.isInteger(value) ? `#${value}` : ''}, splitLine: {lineStyle: {color: '#e7eae4', type: 'dashed'}}, axisLine: {show: false}, axisTick: {show: false}},
      series: data.map((series, index) => {
        const color = colorOf(options, series.id), values = ranks[index];
        const finalRank = values.at(-1);
        return {
          id: String(series.id), name: nameOf(series, options), type: 'line', data: values,
          connectNulls: false, smooth: false, animation: false,
          showSymbol: true, showAllSymbol: true, symbol: 'circle', symbolSize: 4,
          lineStyle: {width: 2.4, color}, itemStyle: {color, borderColor: paper, borderWidth: .5},
          emphasis: {focus: 'series', scale: true, lineStyle: {width: 3.5}}, blur: {lineStyle: {opacity: .14}, itemStyle: {opacity: .2}},
          endLabel: {show: finalRank !== null && finalRank !== undefined, formatter: () => `${series.id}  #${finalRank}`, color, fontSize: mobile ? 12 : 13, distance: 7, backgroundColor: paper, padding: [2, 3], borderRadius: 2},
          labelLayout: {moveOverlap: 'shiftY', hideOverlap: true},
          z: 2
        };
      }),
      graphic: visibleRanks.length ? [] : [{type: 'text', left: 'center', top: 'middle', style: {text: '当前范围无有效全局排名\n可切换分数查看，或调整统计范围', textAlign: 'center', lineHeight: 23, fill: muted, font: '14px Microsoft YaHei, sans-serif'}}]
    };
  }

  root.OpenAlexBump = Object.freeze({buildOption, tooltipRows, definition, years});
})(typeof window === 'object' ? window : globalThis);
