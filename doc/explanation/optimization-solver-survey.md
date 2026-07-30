---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> **Provenance.** Compiled 2026-07-30 from vendor release notes, project
> READMEs, package indexes and paper abstracts. Provenance is uneven: solver
> capabilities and the algorithmic descriptions were read from primary
> documentation, but the comparative speedup figures (cuPDLPx, Gurobi 13.0,
> SOAP, Muon) are quoted from the announcing party and have not been
> independently reproduced. Nothing here has been benchmarked against boxer
> workloads, and no solver below is currently a boxer dependency. Treat it as
> an orientation map, not a measurement.

# Optimization solvers by problem class — a survey

Picking an optimizer is mostly a classification problem: name the structure you
actually have, and the choice of solver follows from it with little remaining
freedom. Getting the classification wrong is the dominant failure mode — far
more costly than picking the second-best solver inside the right class, because
a mismatched class silently returns a plausible wrong answer instead of failing.

This document surveys the classes and their current defaults. It is a snapshot
of an ecosystem that moves at very different speeds in different places: the
linear-programming entry below changed more between 2023 and 2026 than in the
preceding twenty years, while the entry for bound-constrained smooth
minimization has had the same answer since 1995.

## Two shifts that reframe the choice

**Automatic differentiation has shrunk the derivative-free category.** If the
objective is code you can compile, a modern AD tool turns a black-box problem
into a gradient problem, after which quasi-Newton methods dominate any
derivative-free method by a wide margin. Much of what was genuinely
derivative-free in 1973 is now merely undifferentiated. Check this before
reaching for anything in the derivative-free entry.

**GPUs entered classical optimization, but only through first-order methods,
and only for LP and conic problems — not for branch-and-bound.** Simplex and
interior-point methods rest on matrix factorizations, which are sequential and
resist parallelization; first-order methods are matrix-vector products with no
factorization at all, which is what GPU memory hierarchies reward.
Branch-and-bound is irregular, sequential and memory-bound, and does not
benefit the same way. Gurobi has published on precisely this asymmetry. Expect
no GPU windfall on integer problems.

## Reading the entries

Each entry gives the problem's name and synonyms, its defining conditions, what
it is used for, the current solver landscape, and links to papers and
implementations. Implementations are tagged with their language where that is
not obvious; **Julia**, **Rust**, **Go** and **Fortran** availability is called
out explicitly, since all four are uneven across classes and are summarized
again at the end. Python is the unmarked default — where an entry names a
library with no language tag, it is a Python one.

---

## 1. Linear programming

**Also known as** linear optimization, LP; *LP relaxation* when it arises as a
subproblem inside a mixed-integer solve.

**Problem.** Minimize `cᵀx` subject to `Ax ≤ b` and `l ≤ x ≤ u`, with the
objective and every constraint affine and every variable continuous. The
feasible region is a convex polyhedron, so every local optimum is global, and
if an optimum exists then one exists at a vertex.

**Use cases.** Blending and diet problems, production and capacity planning,
network flow, transportation and assignment, cutting stock, and portfolio
construction under linear risk proxies. LP is also the dominant *subproblem*:
every node of a branch-and-bound tree is an LP solve, so LP throughput sets MIP
throughput.

**Landscape.** HiGHS is the open-source workhorse — dual simplex plus interior
point, and now the solver behind SciPy's `linprog` and CVXPY. It absorbed a
PDLP implementation in v1.7.0 (March 2024). For very large LPs, or when a GPU
is available, PDLP and cuPDLPx are primal-dual hybrid gradient methods that
skip factorization entirely; cuPDLPx reports 2.5–5× speedups on MIPLIB LP
relaxations and 3–6.8× on Mittelmann's set relative to its predecessors, and
the line has extended to multi-GPU and to GPU presolve. Commercially, Gurobi
13.0 (November 2025) shipped its own PDHG implementation with both CPU and GPU
support. The caveat that decides the choice: first-order methods reach moderate
accuracy quickly and high accuracy slowly, so if an exact vertex solution or a
basis for warm-starting a MIP is needed, simplex still wins.

**Links.**
- [cuPDLPx: A Further Enhanced GPU-Based First-Order Solver for LP](https://arxiv.org/pdf/2507.14051) — paper
- [Scaling up linear programming with PDLP](https://research.google/blog/scaling-up-linear-programming-with-pdlp/) — Google Research overview
- [An Overview of GPU-based First-Order Methods for LP and Extensions](https://arxiv.org/pdf/2506.02174) — survey
- [D-PDLP: Scaling PDLP to Distributed Multi-GPU Systems](https://arxiv.org/pdf/2601.07628) · [Presolving for GPU-Accelerated First-Order LP Solvers](https://arxiv.org/pdf/2604.23951)
- [HiGHS](https://highs.dev/) — C++, open source, the default open LP/MIP solver
- [cuPDLPx](https://github.com/MIT-Lu-Lab/cuPDLPx) (CUDA/C) · [cuPDLP-C](https://github.com/COPT-Public/cuPDLP-C) · [cuPDLP.jl](https://github.com/jinwen-yang/cuPDLP.jl) (Julia)
- **Julia:** `HiGHS.jl` and `Tulip.jl` (a pure-Julia interior-point method) through JuMP; [cuPDLP.jl](https://github.com/jinwen-yang/cuPDLP.jl) for the GPU path
- **Rust:** [good_lp](https://github.com/rust-or/good_lp) models LPs over pluggable backends — [Clarabel](https://crates.io/crates/clarabel) and `microlp` are pure Rust; HiGHS, CBC and lp_solve are C/C++ bindings
- **Go:** [`gonum.org/v1/gonum/optimize/convex/lp`](https://pkg.go.dev/gonum.org/v1/gonum/optimize/convex/lp) — a dense simplex, adequate for small problems only; [golp](https://github.com/draffensperger/golp) wraps LPSolve over cgo

## 2. Mixed-integer programming

**Also known as** MIP, MILP (linear), MINLP (nonlinear), integer programming,
combinatorial optimization when the model is naturally discrete.

**Problem.** An LP (or NLP) in which some subset of the variables is
constrained to take integer values. Integrality destroys convexity, so the
problem is NP-hard in general and is solved by branch-and-bound over the
continuous relaxation, strengthened by cutting planes, presolve and heuristics.

**Use cases.** Facility location, unit commitment in power systems, crew and
vehicle scheduling, network design, lot sizing, and any model containing a
genuine yes/no decision — open a warehouse, run a generator, assign a shift.
The integrality is usually the entire point; relaxing it gives an answer nobody
can act on.

**Landscape.** Gurobi remains the default, with version 13.0 claiming roughly
16% faster solves on hard MIPs and over 2× on MINLPs. COPT and Xpress are real
alternatives, worth benchmarking on your own instances rather than on vendor
slides. Open source: HiGHS for MILP, and SCIP when you need to customize
branching rules or constraint handlers, or when the model is genuinely MINLP.
The gap between open-source and commercial MILP on hard instances is real and
openly discussed by the HiGHS maintainers themselves — budget for it rather
than being surprised by it. Note again that GPUs do not transfer here.

**Links.**
- [Gurobi 13.0 release notes](https://www.gurobi.com/news/gurobi-releases-version-13-0-with-improved-performance-and-new-solving-capabilities/) — vendor
- [Using GPUs to Solve LPs vs. MIPs: What's the Difference?](https://www.gurobi.com/resources/blog/using-gpus-to-solve-lps-vs-mips-what-s-the-difference) — the LP/MIP asymmetry, stated by a vendor with no incentive to understate GPUs
- [Why are open source MIP solvers slower than commercial ones?](https://github.com/ERGO-Code/HiGHS/discussions/1683) — maintainers' own answer
- [SCIP](https://www.scipopt.org/) — C, open source, customizable, MINLP-capable
- [MIPLIB](https://miplib.zib.de/) — the standard instance library
- **Julia:** JuMP with `HiGHS.jl`, `SCIP.jl` or `Cbc.jl`, or commercial `Gurobi.jl` / `CPLEX.jl` / `Xpress.jl`; `Juniper.jl` for MINLP. Swapping backends is a one-line change, which makes JuMP a good place to benchmark solvers against your own instances.
- **Rust:** [good_lp](https://github.com/rust-or/good_lp) — every backend it exposes supports integer variables, and SCIP is reachable through its optional `russcip` dependency
- **Go:** no native solver. [OR-Tools MathOpt](https://developers.google.com/optimization) exposes HiGHS/Gurobi/SCIP backends and has an informally-supported Go surface; [golp](https://github.com/draffensperger/golp) for small MILPs over cgo

## 3. Constraint programming, scheduling and routing

**Also known as** CP, CP-SAT, constraint satisfaction (CSP) when there is no
objective, lazy clause generation for the modern hybrid form.

**Problem.** Find an assignment to finite-domain variables satisfying a set of
constraints, optionally optimizing an objective. The distinguishing feature is
not the mathematics but the *modelling vocabulary*: global constraints such as
`AllDifferent`, `NoOverlap`, `Cumulative` and `Circuit` carry dedicated
propagators, so the solver exploits combinatorial structure that would be
invisible in a constraint matrix.

**Use cases.** Job-shop and flow-shop scheduling, employee rostering, school
and sports timetabling, bin packing, and vehicle routing. The rule of thumb:
if the problem is more naturally stated as "these two tasks must not overlap"
or "exactly one of these" than as a matrix of coefficients, this is the class.

**Landscape.** OR-Tools CP-SAT is the workhorse, and it is not close. It is
lazy clause generation over a SAT core, with integer and interval variables and
native scheduling and routing constraints, and it has taken essentially every
gold medal in the MiniZinc Challenge since its 2017 debut. It is Apache-2
licensed and free, and Google's own reporting claims it beats commercial
solvers on small-to-medium scheduling. Switch away when the problem is
genuinely continuous, or when the LP relaxation is doing all the work — then it
is a MIP.

**Links.**
- [CP-SAT solver description (MiniZinc Challenge)](https://www.minizinc.org/challenge/2024/description_or-tools_cp-sat.txt) — architecture, from the authors
- [CP-SAT for scheduling — Laurent Perron](https://schedulingseminar.com/presentations/SchedulingSeminar_LaurentPerron.pdf) — slides
- [The CP-SAT Primer](https://d-krupke.github.io/cpsat-primer/) — book-length practical guide to modelling for CP-SAT
- [MiniZinc Challenge](https://www.minizinc.org/challenge/) — the benchmark that decides this class
- [OR-Tools](https://github.com/google/or-tools) — C++ core with Python/Java/.NET bindings
- **Julia:** `MiniZinc.jl` is the practical route, since it reaches CP-SAT and every other MiniZinc backend from JuMP; `JaCoP.jl` wraps a native Java solver. There is no pure-Julia CP solver worth preferring to CP-SAT.
- **Rust:** nothing comparable. Calling CP-SAT over FFI is the realistic path.
- **Go:** [`github.com/google/or-tools/ortools/sat/go`](https://pkg.go.dev/github.com/google/or-tools/ortools/sat/go) — a real CP-SAT model builder, but support is Bazel-oriented and informal; see [issue #5042](https://github.com/google/or-tools/issues/5042) requesting formal Go support. This is the one class where Go is genuinely served.

## 4. Convex conic programming

**Also known as** conic optimization; by cone: QP, SOCP (second-order cone),
SDP (semidefinite), EXP (exponential cone); *disciplined convex programming*
(DCP) for the modelling discipline that reduces to it.

**Problem.** Minimize a linear (or convex quadratic) objective subject to
membership in a convex cone. Convexity is the whole asset: any local optimum is
global, duality gives a certificate of optimality, and interior-point methods
converge in a number of iterations nearly independent of problem size.

**Use cases.** Portfolio optimization with variance or tracking-error terms,
robust control and Lyapunov analysis, filter design, maximum-entropy and
logistic estimation, relaxations of combinatorial problems, and experiment
design. In statistics and machine learning, most regularized estimators
(LASSO, elastic net, SVM) are conic programs in disguise.

**Landscape.** The modelling layer is the workhorse here — CVXPY in Python,
JuMP in Julia — because they perform the reduction to conic standard form,
which is most of the difficulty. Underneath, Clarabel is a Rust and Julia
interior-point solver for conic programs with quadratic objectives, and has
been CVXPY's default for LP and SOCP since version 1.5. Use SCS for large
problems where low accuracy suffices, and MOSEK commercially, particularly for
semidefinite programs. The discipline is worth the effort: if the problem can
be forced into DCP form, the result is a global optimum with a certificate,
which no nonconvex method will ever provide.

**Links.**
- [Clarabel: An interior-point solver for conic programs with quadratic objectives](https://link.springer.com/article/10.1007/s12532-026-00320-7) — *Mathematical Programming Computation*, 2026 ([preprint](https://arxiv.org/pdf/2405.12762))
- [Clarabel](https://github.com/oxfordcontrol/Clarabel.rs) — **Rust**, with Julia and Python bindings
- [CVXPY](https://www.cvxpy.org/) (Python) · [JuMP](https://jump.dev/) (Julia) — modelling layers
- [CVXPY solver features matrix](https://www.cvxpy.org/tutorial/solvers/index.html) — which backend supports which cone
- [SCS](https://github.com/cvxgrp/scs) — C, first-order, large and low-accuracy
- **Julia:** [JuMP](https://jump.dev/) plus `Clarabel.jl`, `SCS.jl`, `COSMO.jl` or `Hypatia.jl` (pure Julia, non-symmetric cones). See the [JuMP solver index](https://jump.dev/JuMP.jl/stable/packages/solvers/) for the current cone-support matrix.
- **Rust:** [Clarabel](https://crates.io/crates/clarabel) is a native Rust crate rather than a binding — LP, QP, SOCP, SDP plus exponential and power cones. No integer variables. Since Clarabel is also CVXPY's default, a Rust program can reach the same solver the Python recommendation points at, without Python in the loop.
- **Go:** nothing. Conic modelling and solving are absent from the Go ecosystem.

## 5. Quadratic programming and embedded model-predictive control

**Also known as** QP; MPC, receding-horizon control, real-time optimization
when it is the control loop.

**Problem.** Minimize `½xᵀPx + qᵀx` subject to linear constraints, with `P`
positive semidefinite. A special case of §4, separated here because the
*deployment* constraints are different in kind: the same small QP is solved
every control period, with a hard latency deadline, on hardware with no
dynamic allocation.

**Use cases.** Model-predictive control of vehicles, drones, robots, process
plants and power converters; real-time trajectory tracking; portfolio
rebalancing under turnover limits. The defining characteristic is that the
problem barely changes between solves, so warm-starting from the previous
solution is worth more than raw solver speed.

**Landscape.** OSQP is the default — ADMM-based, small, warm-startable,
code-generatable, and division-free at runtime. For nonlinear MPC and optimal
control, acados wraps SQP around fast QP backends. For one-shot QPs where
accuracy matters more than latency, use Clarabel or HiGHS instead. The property
that decides this class is not asymptotic speed but warm-start quality and a
bounded worst-case iteration count: a solver twice as fast on average but
occasionally taking 50 ms is useless in a 10 ms control loop.

**Links.**
- [OSQP: An Operator Splitting Solver for Quadratic Programs](https://osqp.org/citing/) — paper and solver, **C** with embedded code generation
- [acados](https://github.com/acados/acados) — C, nonlinear MPC and optimal control
- **Julia:** `OSQP.jl` and `DAQP.jl` through JuMP; note that Julia's startup and JIT behaviour make it a poor fit for the hard-real-time end of this class regardless of solver quality.
- **Rust:** [Clarabel](https://crates.io/crates/clarabel) covers QP natively, and Rust's lack of a runtime makes it a more natural fit here than Julia or Go.
- **Go:** nothing native. OSQP's C API is small and cgo-wrappable if a Go control loop ever needs it.

## 6. Smooth constrained nonlinear programming

**Also known as** NLP, constrained nonlinear optimization; SQP and
interior-point by method family.

**Problem.** Minimize a smooth `f(x)` subject to smooth equality and inequality
constraints, with no convexity assumed. First and ideally second derivatives of
both objective and constraints are required. Only local optimality is
guaranteed, and it depends on constraint qualifications holding at the solution.

**Use cases.** Trajectory optimization and optimal control, AC optimal power
flow, chemical process design, structural and aerodynamic shape optimization,
and parameter estimation in mechanistic models. These are typically large,
sparse and highly structured, and exploiting that structure is the difference
between minutes and days.

**Landscape.** Ipopt is still the workhorse after twenty years — filter
line-search interior point. Pair it with a good sparse linear solver (HSL's
MA57 or MA97 if obtainable, MUMPS otherwise), because that choice often matters
more than anything else. MadNLP is the interesting successor: the same filter
line-search interior-point lineage, restructured to exploit GPU and device
memory, and worth reaching for on large structured problems. KNITRO
commercially. State the real workhorse honestly, though: it is CasADi *plus*
Ipopt, not Ipopt alone. CasADi supplies exact first and second derivatives from
a symbolic graph, and Ipopt without exact Hessians is a much weaker tool.

**Links.**
- [Ipopt](https://github.com/coin-or/Ipopt) — C++, with **Fortran** linear-solver backends (HSL, MUMPS)
- [MadNLP](https://github.com/MadNLP/MadNLP.jl) — Julia, GPU-capable
- [CasADi](https://web.casadi.org/) — C++ with Python/MATLAB/Octave bindings; symbolic AD front-end
- [SLSQP](https://github.com/jacobwilliams/slsqp) — **modern Fortran**, object-oriented rewrite of Kraft's SQP code; a good small-problem choice ([PySLSQP paper](https://arxiv.org/pdf/2408.13420))
- [CONMIN](https://github.com/jacobwilliams/conmin) — **modern Fortran**, feasible-directions method
- **Julia:** `Ipopt.jl`, [MadNLP.jl](https://github.com/MadNLP/MadNLP.jl), `NLopt.jl` and commercial `KNITRO.jl`, all behind JuMP's single modelling surface with derivatives supplied automatically. This is the class where Julia's integration pays off most.
- **Rust:** nothing. [argmin](https://argmin-rs.org/) is unconstrained by design, so this class has no Rust-native answer.
- **Go:** nothing. [`gonum.org/v1/gonum/optimize`](https://pkg.go.dev/gonum.org/v1/gonum/optimize) explicitly supports no constraints of any kind.

## 7. Nonlinear least squares

**Also known as** NLLS, curve fitting, data fitting, bundle adjustment in
vision, parameter estimation in statistics.

**Problem.** Minimize `½‖r(x)‖²` for a vector of residuals `r`. The
sum-of-squares structure is what distinguishes this from §6: the Gauss–Newton
approximation `JᵀJ` supplies curvature from the Jacobian alone, so second
derivatives never need computing. Trust-region and Levenberg–Marquardt methods
exploit exactly this.

**Use cases.** Fitting model parameters to measurements, camera calibration and
structure-from-motion, SLAM back-ends, kinetic and pharmacokinetic model
fitting, and sensor fusion. Robust loss functions matter here more than
almost anywhere else, because real measurement sets contain outliers that
squared error weights catastrophically.

**Landscape.** Ceres Solver for bundle adjustment, SLAM and geometry — a
trust-region implementation with sparse Schur-complement elimination and a
library of robust losses. `scipy.optimize.least_squares` with the `trf` method
for anything smaller. DFO-LS when residuals are a black box or noisy. The
recurring mistake in this class is handing a least-squares problem to a general
NLP solver, which then spends real function evaluations estimating curvature
that the Jacobian was already carrying for free.

**Links.**
- [Ceres Solver](http://ceres-solver.org/) — C++, the standard for large sparse geometry problems
- [MINPACK](https://github.com/fortran-lang/minpack) — the **Fortran** original (Moré, Garbow, Hillstrom), modernized under fortran-lang; [Jacob Williams' edition](https://github.com/jacobwilliams/minpack) adds CMake and examples
- [DFO-LS](https://github.com/numericalalgorithmsgroup/dfols) — Python, derivative-free and noise-tolerant
- **Julia:** `LsqFit.jl` for curve fitting; [NonlinearSolve.jl](https://github.com/SciML/NonlinearSolve.jl) in the SciML stack for the systems-of-equations side
- **Rust:** [levenberg-marquardt](https://crates.io/crates/levenberg-marquardt) — a port of MINPACK's LM over `nalgebra` — and argmin's `gaussnewton` module. One of the few classes where Rust is better served than Go.
- **Go:** a genuine gap — [`gonum.org/v1/gonum/optimize`](https://pkg.go.dev/gonum.org/v1/gonum/optimize) provides no Levenberg–Marquardt or dedicated least-squares method. Generic BFGS over the squared residual is the available fallback and forfeits the structure.

## 8. Smooth unconstrained and bound-constrained minimization

**Also known as** unconstrained optimization, box-constrained optimization,
quasi-Newton methods; the "just minimize this function" case.

**Problem.** Minimize a smooth `f(x)`, optionally with `l ≤ x ≤ u`. Gradients
are available or obtainable. No other constraints. This is the best-understood
problem in the whole survey and the one where the answer has been stable
longest.

**Use cases.** Maximum-likelihood estimation, training of classical statistical
models, energy minimization in molecular and physical simulation, image
registration, and the inner loop of countless larger algorithms.

**Landscape.** L-BFGS-B. Still. Limited-memory, handles bounds, and hardened by
three decades of use — it is the single highest-value default in numerical
optimization. Use a trust-region Newton-CG method when Hessian-vector products
are available. The important advice is not about the optimizer but the
derivative: obtain the gradient from automatic differentiation rather than
finite differences. Finite differences cost `n` extra evaluations per gradient
and introduce a step-size trade-off between truncation and rounding error that
has no good resolution.

**Links.**
- [L-BFGS-B](https://users.iems.northwestern.edu/~nocedal/lbfgsb.html) — Byrd, Lu, Nocedal, Zhu; the reference **Fortran** implementation
- [JAX](https://github.com/jax-ml/jax) · [Enzyme](https://enzyme.mit.edu/) · [CasADi](https://web.casadi.org/) — AD front-ends worth more than any optimizer choice
- **Julia:** [Optim.jl](https://github.com/JuliaNLSolvers/Optim.jl) for the classical methods, and [Optimization.jl](https://github.com/SciML/Optimization.jl) as a common interface that composes with Julia's AD backends — so the gradient advice above is satisfied by default rather than by extra work
- **Rust:** [argmin](https://argmin-rs.org/) provides `quasinewton` (L-BFGS, BFGS), `conjugategradient`, `newton`, `trustregion`, `gradientdescent` and `linesearch`, generic over `Vec`, `ndarray` and `nalgebra`. Unconstrained only, and gradients are the caller's problem — Rust has no mature AD either.
- **Go:** [`gonum.org/v1/gonum/optimize`](https://pkg.go.dev/gonum.org/v1/gonum/optimize) provides `LBFGS`, `BFGS`, `CG` (five β variants), `Newton` and `GradientDescent` — unconstrained only, so bounds must be handled by transformation. Gradients via [`gonum.org/v1/gonum/diff/fd`](https://pkg.go.dev/gonum.org/v1/gonum/diff/fd) are finite differences; Go has no mature AD.

## 9. Derivative-free local optimization

**Also known as** DFO, black-box optimization (local), zeroth-order
optimization, direct search.

**Problem.** Minimize `f(x)` where only function values are available — no
gradient, and none obtainable by AD because the objective is a legacy binary, a
simulation, or a physical experiment. Assumptions about smoothness and noise
determine which sub-method applies, and getting those assumptions wrong is the
main failure mode.

**Use cases.** Tuning simulation parameters, calibrating legacy engineering
codes, hyperparameter search where evaluations are cheap enough to run
thousands, and any objective behind an interface you cannot differentiate
through. Reach for this class only after confirming AD is genuinely unavailable.

**Landscape.** Smooth with bounds: BOBYQA. Smooth with nonlinear constraints:
COBYLA. Both are Powell's interpolation-based trust-region methods, and both
should be taken from PRIMA rather than from older ports — PRIMA is a faithful
modern-Fortran reimplementation that fixes bugs in the F77 originals, whereas
several widely-used copies are mechanical translations that, in PRIMA's own
words, inherit the originals' "style, structure, and probably bugs". Noisy
objectives: Py-BOBYQA, or DFO-LS for least squares, both of which add sample
averaging, regression models and restart-on-stagnation. Ill-conditioned, rugged
or multimodal with evaluations to spare: CMA-ES. Nonsmooth, or where a
convergence guarantee is required: NOMAD, which implements MADS and converges
to Clarke stationary points.

Brent's PRAXIS deserves a note as the historical member of this class: Powell's
conjugate-direction method with a periodic SVD that rotates the search
directions onto the principal axes of the local quadratic model. It remains
sound on smooth, deterministic, unconstrained, moderately-sized and
ill-conditioned problems, and its small dependency-free footprint keeps it in
use. It fails silently under noise, because its parabolic line search fits a
meaningless minimum and then reports convergence.

**Links.**
- [PRIMA](https://github.com/libprima/prima) — **modern Fortran** reference implementations of COBYLA, UOBYQA, NEWUOA, BOBYQA, LINCOA, with C, Python, MATLAB and Julia bindings
- [Improving the Flexibility and Robustness of Model-Based DFO Solvers](https://dl.acm.org/doi/10.1145/3338517) — *ACM TOMS*; the noise-handling results ([preprint](https://arxiv.org/pdf/1804.00154))
- [Py-BOBYQA](https://numericalalgorithmsgroup.github.io/pybobyqa/build/html/index.html) · [DFO-LS](https://github.com/numericalalgorithmsgroup/dfols) — Python, noise-tolerant
- [NOMAD](https://github.com/bbopt/nomad) — C++, MADS with convergence theory
- [Benchmarking Derivative-Free Optimization Algorithms](https://epubs.siam.org/doi/10.1137/080724083) — Moré and Wild; the standard methodology
- [Derivative-free optimization: a review and comparison of software](https://www.researchgate.net/publication/257588610_Derivative-free_optimization_A_review_of_algorithms_and_comparison_of_software_implementations) — Rios and Sahinidis
- [PRAXIS](https://link.springer.com/article/10.3758/BF03203605) — Gegenfurtner's account of Brent's algorithm; Brent, *Algorithms for Minimization without Derivatives* (Dover, 2002, ISBN 0-486-41998-3)
- **Julia:** [PRIMA.jl](https://github.com/libprima/PRIMA.jl) wraps PRIMA directly, which makes it the recommended source for BOBYQA and COBYLA rather than an older port; `NLopt.jl` for the wider derivative-free set
- **Rust:** [argmin](https://argmin-rs.org/) has `neldermead` only. [egobox](https://github.com/relf/egobox) reaches COBYLA and SLSQP through an optional `nlopt` feature, which is a binding rather than a Rust implementation.
- **Go:** [`gonum.org/v1/gonum/optimize`](https://pkg.go.dev/gonum.org/v1/gonum/optimize) provides `NelderMead` and `CmaEsChol`. No Powell-family method exists in Go.

## 10. Expensive black-box optimization and hyperparameter search

**Also known as** Bayesian optimization, BO, HPO, sequential model-based
optimization, design of experiments, adaptive experimentation.

**Problem.** Minimize `f(x)` where each evaluation costs minutes to hours, the
total budget is tens to hundreds of evaluations, observations may be noisy, and
the search space may mix continuous, integer, categorical and conditional
parameters. The expense is the defining condition: it justifies spending
substantial computation deciding where to sample next.

**Use cases.** Machine-learning hyperparameter tuning, simulation-based design,
A/B and physical experiment design, compiler and database configuration, and
materials or drug candidate screening. The parameter space is usually small
(under ~20 dimensions) and the evaluations genuinely costly.

**Landscape.** Optuna is the pragmatic default — TPE sampling with pruning of
unpromising trials, trivial to instrument into existing training code, and
OptunaHub now distributes samplers as installable modules. Ax (1.0 released
December 2025) when the Bayesian machinery should be chosen for you, with
BoTorch underneath for writing acquisition functions directly. Nevergrad for
evolutionary portfolios, SMAC for algorithm configuration over mixed and
conditional spaces. The regime marker matters: if evaluations are *cheap*, this
is the wrong class — the sampling overhead dominates, and CMA-ES or a
model-based method from §9 will do better.

**Links.**
- [OptunaHub: A Platform for Black-Box Optimization](https://arxiv.org/pdf/2510.02798) — paper
- [Ax: A Platform for Adaptive Experimentation](https://openreview.net/pdf?id=U1f6wHtG1g) — paper; [1.0 release coverage](https://www.infoq.com/news/2025/12/ax-hyperparameter-optimization/)
- [Optimizing with Low Budgets: a Comparison on BBOB and OpenAI Gym](https://arxiv.org/pdf/2310.00077) — cross-library comparison
- [AutoML HPO tool overview](https://www.automl.org/hpo-overview/hpo-tools/hpo-packages/) — index
- [Optuna](https://github.com/optuna/optuna) · [Ax/BoTorch](https://github.com/facebook/Ax) · [Nevergrad](https://github.com/facebookresearch/nevergrad) · [SMAC3](https://github.com/automl/SMAC3) — all Python
- **Julia:** `Hyperopt.jl` — random search, Latin hypercube sampling and Bayesian optimization
- **Rust:** [egobox](https://github.com/relf/egobox) — efficient global optimization with Gaussian-process mixtures and sampling methods, and the strongest Rust entry in this survey
- **Go:** [goptuna](https://github.com/c-bata/goptuna) — a pure-Go Optuna-alike with TPE, CMA-ES and bandit samplers, continuously benchmarked in CI. The strongest Go offering in this survey after CP-SAT.

## 11. Stochastic optimization for machine learning

**Also known as** SGD and its variants, stochastic approximation, empirical
risk minimization, "training".

**Problem.** Minimize `E[f(x, ξ)]` — an expectation over data — where the full
objective is never evaluated, only unbiased minibatch estimates of its
gradient. Dimensions run to billions, the objective is nonconvex, and the goal
is generalization rather than optimality, so the optimizer is a
regularizer as much as a minimizer.

**Use cases.** Training neural networks of every kind. The class is
characterized less by the mathematics than by the operating point: enormous
dimension, cheap noisy gradients, a fixed compute budget, and a success metric
measured on held-out data rather than on the training objective.

**Landscape.** AdamW remains the default and the safe answer. Muon is the first
serious challenger in a decade: it orthogonalizes the momentum update via
Newton–Schulz iteration, treating weight matrices as matrices with geometric
structure rather than as bags of independent scalars. It shipped natively as
`torch.optim.Muon` in PyTorch 2.9 and subsequently in DeepSpeed and NVIDIA
NeMo; reported production runs include Kimi K2 (1T parameters), GLM-4.5 (355B)
and INTELLECT-3 (106B); and NVIDIA reported in April 2026 near-parity training
throughput with AdamW on GB300 NVL72 hardware with better resulting model
quality. SOAP — Shampoo with Adam in the preconditioner's eigenbasis — is the
second-order option, reported at over 40% fewer steps and roughly 35% less
wall-clock than AdamW in the large-batch regime, at higher per-step overhead.

**Links.**
- [SOAP: Improving and Stabilizing Shampoo using Adam](https://arxiv.org/pdf/2409.11321) — paper
- [NVIDIA Megatron with the Muon optimizer](https://blockchain.news/news/nvidia-megatron-muon-llm-training) — April 2026 throughput report
- [Navigating LLM Valley: From AdamW to Memory-Efficient and Matrix-Based Optimizers](https://arxiv.org/pdf/2605.09176) — 2026 survey of the family
- [Towards Robust Scaling Laws for Optimizers](https://arxiv.org/pdf/2602.07712) — how these comparisons should be run
- [Optax](https://github.com/google-deepmind/optax) (JAX) · [`torch.optim`](https://pytorch.org/docs/stable/optim.html) (PyTorch)
- **Julia, Rust, Go:** all three have neural-network frameworks, but none is where frontier training happens. Muon and SOAP land in PyTorch and JAX first and reach the others late or not at all, so an optimizer choice made here is a choice to track someone else's roadmap.

## 12. Global optimization

**Also known as** GO; *deterministic* global optimization when a bound is
proven, *stochastic* or *heuristic* global optimization when it is not.

**Problem.** Find the global minimum of a multimodal `f(x)`, not merely a local
one. The class splits sharply in two, and conflating the halves is the most
common error: heuristic methods return a good point with no guarantee, while
spatial branch-and-bound returns a point *plus a proven bound* on how far from
optimal it can be. The second costs orders of magnitude more.

**Use cases.** Molecular conformation, phase equilibrium, parameter estimation
with many local minima, engineering design over rugged response surfaces, and
any model where a local solver's answer depends visibly on its starting point.
The proven-bound half is used where a certificate is a deliverable — safety
cases, contractual guarantees, published bounds.

**Landscape.** For the heuristic half on cheap continuous functions, CMA-ES
with restarts (IPOP or BIPOP) is the workhorse; SciPy's `differential_evolution`
and `dual_annealing` serve for quick work. For the certified half, SCIP, BARON
or Couenne perform spatial branch-and-bound over a structured MINLP. Decide
which half you are in before choosing, because the answer to "did it work" is
different in each: a heuristic that finds the optimum cannot tell you it did.

**Links.**
- [CMA-ES tutorial](https://arxiv.org/abs/1604.00772) — Hansen; the standard reference
- [COCO/BBOB](https://numbbo.github.io/coco/) — the benchmarking platform for this class
- [SCIP](https://www.scipopt.org/) — C, deterministic global MINLP, open source
- [pycma](https://github.com/CMA-ES/pycma) — the reference CMA-ES implementation, Python
- **Julia:** [BlackBoxOptim.jl](https://github.com/robertfeldt/BlackBoxOptim.jl) — meta-heuristic and stochastic methods (differential evolution, natural evolution strategies), single- and multi-objective, with no differentiability requirement; `SCIP.jl` for the certified half
- **Rust:** [argmin](https://argmin-rs.org/) carries `particleswarm` and `simulatedannealing`, but no CMA-ES
- **Go:** [`CmaEsChol`](https://pkg.go.dev/gonum.org/v1/gonum/optimize#CmaEsChol) in gonum implements CMA-ES via a Cholesky-factor update, reducing the per-generation cost from `O(d³)` to `O(d²·popsize)`. This is the most substantive optimizer in the Go ecosystem.

---

## The Julia ecosystem, summarized

Julia has the most complete coverage of any language in this survey after
Python, and it is the only one where the *modelling layer* rather than the
solvers is the distinguishing asset.

[JuMP](https://jump.dev/) sits on MathOptInterface, which defines the API
solvers implement, supplies a bridge system that automatically reformulates a
problem into whatever form the chosen solver actually accepts, and carries
shared test infrastructure that solver authors run against. The practical
consequence is that changing solver is close to a one-line edit, which makes
JuMP a good place to benchmark candidates against your own instances rather
than against a published table. Alongside it,
[Optimization.jl](https://github.com/SciML/Optimization.jl) in the SciML stack
provides a common interface that composes with Julia's AD backends, so an
optimization problem can sit inside a differentiable program — the property
that makes the §8 advice about AD hold by default rather than by extra work.

Coverage is near-complete across the classes above: `HiGHS.jl` and `SCIP.jl`
for LP and MIP, `Clarabel.jl` / `SCS.jl` / `COSMO.jl` / `Hypatia.jl` for conic,
`Ipopt.jl` and [MadNLP.jl](https://github.com/MadNLP/MadNLP.jl) for NLP,
[PRIMA.jl](https://github.com/libprima/PRIMA.jl) for the Powell family,
[BlackBoxOptim.jl](https://github.com/robertfeldt/BlackBoxOptim.jl) for global
search, and `MiniZinc.jl` as a route to CP-SAT. The
[JuMP solver index](https://jump.dev/JuMP.jl/stable/packages/solvers/) is the
current authority on which package supports which class.

Two caveats. Many of these packages are bindings to the same C, C++ and Fortran
codes every other ecosystem calls, so "available in Julia" does not imply a
distinct implementation — the uniform surface is the value, not the solver.
And the pure-Julia solvers (`Tulip.jl`, `Hypatia.jl`, `COSMO.jl`) are better
understood as research vehicles and extension points than as faster
replacements for the incumbents. Julia's startup and JIT behaviour also make it
a poor fit for the hard-real-time end of §5, independent of solver quality.

## The Rust ecosystem, summarized

Rust's coverage is narrow but sharper than Go's, and it has one genuine
advantage: [Clarabel](https://crates.io/crates/clarabel) is a native Rust crate
rather than a binding, and it is the same solver CVXPY defaults to for LP and
SOCP. A Rust program can therefore reach the solver this survey recommends for
convex conic work with no C shim, no Python runtime, and no cgo boundary.

Around it: [good_lp](https://github.com/rust-or/good_lp) models LP and MILP
over pluggable backends (Clarabel and `microlp` pure Rust; HiGHS, CBC and
lp_solve as bindings; SCIP through the optional `russcip` dependency),
[argmin](https://argmin-rs.org/) provides local unconstrained methods —
`quasinewton`, `conjugategradient`, `newton`, `trustregion`, `neldermead`,
`particleswarm`, `simulatedannealing` — generic over `Vec`, `ndarray` and
`nalgebra`, [levenberg-marquardt](https://crates.io/crates/levenberg-marquardt)
ports MINPACK's LM, and [egobox](https://github.com/relf/egobox) covers
Bayesian optimization with Gaussian-process mixtures.

The gaps are the same shape as Go's: no constrained nonlinear programming, no
native Powell-family derivative-free methods, no constraint programming, no
CMA-ES, and no mature automatic differentiation. Rust is a reasonable place to
*embed* a convex or least-squares solve in a larger system, and not yet a place
to do large-scale nonlinear optimization.

This is also the one entry with a plausible path into this repository, which
already carries a Rust component under `rust/`; the others would require a new
runtime.

## The Go ecosystem, summarized

Go's optimization coverage is thin and unevenly distributed, and it is worth
knowing the shape before planning around it.

What exists and is usable: [`gonum.org/v1/gonum/optimize`](https://pkg.go.dev/gonum.org/v1/gonum/optimize)
provides `BFGS`, `LBFGS`, `CG`, `Newton`, `GradientDescent`, `NelderMead`,
`CmaEsChol`, plus `GuessAndCheck` and `ListSearch` for baselines. A dense
simplex lives in `optimize/convex/lp`. [goptuna](https://github.com/c-bata/goptuna)
covers TPE, CMA-ES and bandit-based hyperparameter search in pure Go. And
OR-Tools ships a genuine CP-SAT model builder for Go.

What is missing, and matters: **no constrained optimization of any kind** in
gonum — not bounds, not linear constraints, not nonlinear ones. **No
Levenberg–Marquardt or dedicated nonlinear least squares**, which is the most
commonly needed algorithm that Go simply does not have. **No mature automatic
differentiation**, so gradients come from finite differences via
[`gonum.org/v1/gonum/diff/fd`](https://pkg.go.dev/gonum.org/v1/gonum/diff/fd)
with the accuracy penalty that implies. No conic or interior-point solver, and
no serious LP or MIP without cgo.

Two practical notes. The standalone `github.com/gonum/optimize` repository is
**deprecated** — the code lives in `gonum/gonum` and is imported as
`gonum.org/v1/gonum/optimize`; the old path still resolves and is a live source
of confusion. And where Go must reach a real solver, the realistic paths are
cgo bindings or an out-of-process call, not a port: OR-Tools, HiGHS and OSQP
all expose small C APIs suited to either.

The honest summary is that Go is a good language for *orchestrating* an
optimization workload and a poor one for *implementing* it. That is a
reasonable division of labour, but it should be a deliberate one.

## The Fortran ecosystem, summarized

Fortran's role in this survey is unusual: it is less a language you would
choose today than the substrate a large fraction of the other entries are
wrapping. LAPACK and BLAS underlie every dense linear-algebra step above;
MINPACK is still the reference for nonlinear least squares; L-BFGS-B, SLSQP and
Powell's derivative-free family were all written in Fortran and have been
reached through bindings ever since.

Two opposing currents are worth noting, because they point in different
directions.

**Modernization.** [PRIMA](https://github.com/libprima/prima) reimplements
Powell's five solvers in modern Fortran, fixing bugs in the F77 originals
rather than translating around them, and exposes C, Python, MATLAB and Julia
interfaces. Jacob Williams has done the same for
[SLSQP](https://github.com/jacobwilliams/slsqp),
[MINPACK](https://github.com/jacobwilliams/minpack) and
[CONMIN](https://github.com/jacobwilliams/conmin), giving each an
object-oriented interface. The [fortran-lang](https://fortran-lang.org/)
community supplies a standard library and a package manager (`fpm`), and
[Beliavsky's package list](https://beliavsky.github.io/Fortran-packages-list/)
indexes what is buildable with it.

**Liquidation.** SciPy has been systematically removing its Fortran
dependencies: MINPACK, L-BFGS-B, SLSQP and NNLS were rewritten in C, and COBYLA
was replaced with a Python translation derived from PRIMA. This is a
maintenance decision rather than a numerical one, but it has had numerical
consequences — the SLSQP rewrite in SciPy 1.16.0 changed both results and
reported success status on some problems, which is a reminder that "same
algorithm, different implementation" is not a safe assumption in this field.

For a new project, Fortran is a reasonable choice only where you are extending
one of these codebases. For everyone else its relevance is indirect: know that
PRIMA is the trustworthy source for Powell's methods, and that a solver's
provenance — original Fortran, faithful modernization, or mechanical
translation — is a real variable in whether it behaves as its paper describes.

## Benchmarks worth trusting

Independent benchmarks, in preference to vendor numbers:
[Mittelmann's benchmarks](https://plato.asu.edu/bench.html) for LP, MIP and
conic; [MIPLIB](https://miplib.zib.de/) for integer instances;
[COCO/BBOB](https://numbbo.github.io/coco/) for continuous black-box methods;
and the [MiniZinc Challenge](https://www.minizinc.org/challenge/) for
constraint programming. All four publish methodology alongside results, which
is the property that makes them worth citing.
