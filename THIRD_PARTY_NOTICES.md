# Third-party materials

The repository retains the upstream licenses for its bundled browser assets.

| Component | Version | License and provenance |
| --- | --- | --- |
| Apache ECharts | 5.6.0 | Apache-2.0; `internal/web/static/vendor/echarts.LICENSE`, `echarts.NOTICE`, and `echarts.LICENSE-d3`; the vendored README records source and SHA-256. |
| flag-icons | 7.6.0 | MIT; `internal/web/static/flags/LICENSE` and `provenance.json` record the exact upstream commit and selected SVG country/territory codes. |

Go dependencies are pinned in `go.mod` and `go.sum`. Their upstream licenses
remain applicable. The build does not vendor dependency source or fetch remote
browser assets at runtime.

The historical Java methodology review links to local source evidence from the
user's previous project; that project's full Java source is not redistributed
here. Public availability of this repository does not replace a project-wide
license grant from its owner.
