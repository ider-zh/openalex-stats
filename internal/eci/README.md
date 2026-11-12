# ECI engine contract

`Compute` receives exactly one year, taxonomy layer and metric as a nonnegative country-by-discipline matrix. Labels must be unique, nonempty and known. The caller must remove quality-only `UNKNOWN` rows/columns. Countries with zero totals and disciplines with zero totals remain in the dictionary but have no score.

The engine calculates `RCA=(Xcd/Xc)/(Xd/X)` and the binary advantage matrix `M=(RCA>=1)`. For positive-diversity countries and positive-ubiquity disciplines, it diagonalizes

Fractional sums can place a mathematically exact RCA=1 at 0.9999999999999998. RCA values within 1e-12 of 1 are therefore normalized to 1 before the standard threshold is applied. This explicit rounding tolerance is recorded in every model result and shared by query-table RCA derivation; it prevents identical proportional portfolios from receiving spurious complexity rankings.

`S = Dc^(-1/2) M Dd^(-1) Mᵀ Dc^(-1/2)`.

The second eigenvector `u` is transformed to the country operator's right eigenvector by `e = Dc^(-1/2)u`. Country scores and the dual discipline projection `p = Dd^(-1)Mᵀe` are population-standardized separately. This avoids applying a symmetric eigensolver to the nonsymmetric country transition matrix. The reported residual is on the original symmetric eigensystem, before standardization.

`ECI` and `PCI` are indexed by `ActiveCountries` and `ActiveDisciplines`, respectively, which refer to the full input label arrays. Inactive entries are omitted, not assigned a misleading zero. Results with fewer than two active rows/columns, disconnected networks, or a repeated second eigenvalue do not receive invented complexity rankings; inspect `Status` (`empty`, `disconnected`, `degenerate`, or `ok`).

Models also report all advantage-network components, largest-component country count, second eigenvalue, nearest-neighbor eigengap, residual and an instability flag. Disconnected models still receive spectral diagnostics but no global scores. A gap below 1e-6 warns of instability; a repeated neighbor within 1e-10 is treated as degenerate. These are numerical diagnostics rather than a claim that a small absolute residual makes a ranking substantively reliable.

Initial sign is oriented toward positive diversity correlation with an input-order tie break. The caller should fix label order and, if desired, apply a declared cross-year sign convention after independent yearly jobs finish. Close eigenvalues can imply unstable ordering even when residuals are small. Results are relative to each year's chosen country set and matrix; annual z-scores do not measure absolute knowledge accumulation.
