"""Read-only independent NumPy audit of frozen original country matrices."""
import hashlib
import json
import sys
from pathlib import Path

import numpy as np

source, output = map(Path, sys.argv[1:])
inputs = json.loads(source.read_text())
checks = []
for item in inputs:
    x = np.asarray(item["matrix"]["values"], dtype=float)
    row, col, total = x.sum(1), x.sum(0), x.sum()
    rca = np.divide(x * total, row[:, None] * col[None, :], out=np.zeros_like(x), where=(row[:, None] * col[None, :]) > 0)
    rca[np.abs(rca - 1) <= 1e-12] = 1
    m = rca >= 1
    base = item["baseline_spectral"]
    assert np.array_equal(m, base["advantage"])
    active_c = np.flatnonzero(m.sum(1))
    active_d = np.flatnonzero(m.sum(0))
    assert active_c.tolist() == base["active_countries"]
    assert active_d.tolist() == base["active_disciplines"]
    mm = m[np.ix_(active_c, active_d)].astype(float)
    kc, kd = mm.sum(1), mm.sum(0)
    t = (mm / kd) @ mm.T / kc[:, None]
    symmetric = (mm / kd) @ mm.T / np.sqrt(kc[:, None] * kc[None, :])
    values, vectors = np.linalg.eigh(symmetric)
    ev = vectors[:, -2] / np.sqrt(kc)
    ev = (ev - ev.mean()) / ev.std()
    original = np.asarray(base["eci"])
    sign = 1 if np.dot(ev, original) >= 0 else -1
    spectral_error = float(np.max(np.abs(sign * ev - original)))
    centered_residual = float(np.max(np.abs((t @ original - (t @ original).mean()) - values[-2] * original)))
    assert spectral_error < 1e-6
    assert centered_residual < 1e-6
    c, p = kc.copy(), kd.copy()
    country16 = None
    for iteration in range(1, 18):
        c, p = mm @ p / kc, mm.T @ c / kd
        if iteration == 16:
            country16 = c.copy()
    # Origin shift avoids cancellation without changing the defined z-score.
    country16 -= country16[0]
    country16 -= country16.mean()
    country16 /= country16.std()
    original_java = np.asarray(item["baseline_java"]["eci"])
    java_sign = 1 if np.dot(country16, original_java) >= 0 else -1
    java_error = float(np.max(np.abs(java_sign * country16 - original_java)))
    assert java_error < 1e-5
    details = []
    if item["corpus"] == "Core" and item["year"] == 2025 and item["scheme"] == "concepts" and item["level"] == "0" and item["metric"] == "citations":
        for code in ["AG", "EH", "SG", "KP", "CN", "US"]:
            i = item["matrix"]["countries"].index(code)
            details.append({"country": code, "advantage_cells": [
                {"discipline": item["matrix"]["disciplines"][j], "fractional_label_input": float(x[i, j]), "rca": float(rca[i, j]), "ubiquity": int(m[:, j].sum())}
                for j in np.flatnonzero(m[i])
            ]})
    checks.append({key: item[key] for key in ["corpus", "year", "scheme", "level", "metric"]} | {
        "M_exact_equal": True, "spectral_max_abs_error_up_to_sign": spectral_error,
        "centered_right_T_eigen_residual_max": centered_residual,
        "java16_max_abs_error_up_to_sign": java_error,
        "spectral_vs_java_country_correlation": float(np.corrcoef(original, original_java)[0, 1]),
        "selected_country_advantage_evidence": details,
    })
result = {"status": "passed_read_only_independent_numpy_check", "matrices": len(checks),
          "source_sha256": hashlib.sha256(source.read_bytes()).hexdigest(),
          "max_spectral_error": max(r["spectral_max_abs_error_up_to_sign"] for r in checks),
          "max_java16_error": max(r["java16_max_abs_error_up_to_sign"] for r in checks),
          "production_changed": False, "checks": checks}
output.write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps({k: v for k, v in result.items() if k != "checks"}))
