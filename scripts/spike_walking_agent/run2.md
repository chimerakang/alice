# Spike #149 Results

Model: `claude-sonnet-4-5-20250929`  Workdir: `/private/tmp/spike-target`  Sub-tasks: 5


## Per-mode totals

| Mode | Calls | Cost (reported) | Cost (5m-norm) | Total input | cache_read | cache_write (1h share) | Output | Wallclock |
|---|---|---|---|---|---|---|---|---|
| baseline | 5 | $0.4026 | $0.4026 | 290,021 | 217,509 (75.0%) | 72,414 (0 @1h) | 4,367 | 133.9s |
| walking | 5 | $0.7393 | $0.2206 | 292,546 | 272,754 (93.2%) | 19,702 (19,702 @1h) | 4,308 | 96.4s |

## Walking vs baseline

- Cost reported (SDK 1h cache vs CLI 5m): **-83.6%** (walking more expensive as billed today)
- **Cost 5m-normalized (apples-to-apples)**: **+45.2%** (walking cheaper if SDK cache TTL matched CLI)
- Cache_creation tokens: **+72.8%**
- Wallclock: **+28.0%**

## Per-turn detail


### baseline

| # | Sub-task | Duration | Input | cache_read | cache_write | Output | Cost |
|---|---|---|---|---|---|---|---|
| 1 | List all top-level files and directories in the current work | 25917ms | 26 | 64057 | 14739 | 918 | $0.0883 |
| 2 | Read the file you saw named CLAUDE.md (or README.md if CLAUD | 17576ms | 18 | 38271 | 14191 | 621 | $0.0741 |
| 3 | Search for any TODO or FIXME comments in *.go files. Report  | 22312ms | 18 | 38342 | 14343 | 810 | $0.0775 |
| 4 | Run `git log --oneline -5` and report the most recent commit | 23922ms | 18 | 38512 | 14499 | 875 | $0.0791 |
| 5 | Cross-reference your previous answers: was the file you read | 44168ms | 18 | 38327 | 14642 | 1143 | $0.0836 |

### walking

| # | Sub-task | Duration | Input | cache_read | cache_write | Output | Cost |
|---|---|---|---|---|---|---|---|
| 1 | List all top-level files and directories in the current work | 20354ms | 18 | 38472 | 14660 | 775 | $0.0782 |
| 2 | Read the file you saw named CLAUDE.md (or README.md if CLAUD | 17064ms | 18 | 54976 | 1841 | 749 | $0.1129 |
| 3 | Search for any TODO or FIXME comments in *.go files. Report  | 16619ms | 18 | 57783 | 1009 | 870 | $0.1471 |
| 4 | Run `git log --oneline -5` and report the most recent commit | 18862ms | 18 | 59761 | 1020 | 776 | $0.1806 |
| 5 | Cross-reference your previous answers: was the file you read | 23496ms | 18 | 61762 | 1172 | 1138 | $0.2206 |
