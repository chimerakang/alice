# Spike #149 Results

Model: `claude-sonnet-4-5-20250929`  Workdir: `/private/tmp/spike-target`  Sub-tasks: 5


## Per-mode totals

| Mode | Calls | Cost (reported) | Cost (5m-norm) | Total input | cache_read | cache_write (1h share) | Output | Wallclock |
|---|---|---|---|---|---|---|---|---|
| baseline | 5 | $0.3956 | $0.3956 | 270,031 | 195,160 (72.3%) | 74,781 (0 @1h) | 3,760 | 106.1s |
| walking | 5 | $0.2060 | $0.2060 | 294,948 | 275,324 (93.3%) | 19,534 (0 @1h) | 3,327 | 79.7s |

## Walking vs baseline

- Cost reported (SDK 1h cache vs CLI 5m): **+47.9%** (walking cheaper as billed today)
- **Cost 5m-normalized (apples-to-apples)**: **+47.9%** (walking cheaper if SDK cache TTL matched CLI)
- Cache_creation tokens: **+73.9%**
- Wallclock: **+24.9%**

## Per-turn detail


### baseline

| # | Sub-task | Duration | Input | cache_read | cache_write | Output | Cost |
|---|---|---|---|---|---|---|---|
| 1 | List all top-level files and directories in the current work | 21344ms | 18 | 38795 | 14658 | 750 | $0.0779 |
| 2 | Read the file you saw named CLAUDE.md (or README.md if CLAUD | 19032ms | 18 | 38853 | 14735 | 563 | $0.0754 |
| 3 | Search for any TODO or FIXME comments in *.go files. Report  | 21905ms | 18 | 39083 | 15097 | 831 | $0.0809 |
| 4 | Run `git log --oneline -5` and report the most recent commit | 24377ms | 18 | 39112 | 14997 | 901 | $0.0815 |
| 5 | Cross-reference your previous answers: was the file you read | 19408ms | 18 | 39317 | 15294 | 715 | $0.0799 |

### walking

| # | Sub-task | Duration | Input | cache_read | cache_write | Output | Cost |
|---|---|---|---|---|---|---|---|
| 1 | List all top-level files and directories in the current work | 20361ms | 18 | 38840 | 15005 | 690 | $0.0783 |
| 2 | Read the file you saw named CLAUDE.md (or README.md if CLAUD | 14326ms | 18 | 55838 | 2002 | 604 | $0.1117 |
| 3 | Search for any TODO or FIXME comments in *.go files. Report  | 14134ms | 18 | 58658 | 848 | 646 | $0.1422 |
| 4 | Run `git log --oneline -5` and report the most recent commit | 12372ms | 18 | 60254 | 725 | 548 | $0.1713 |
| 5 | Cross-reference your previous answers: was the file you read | 18511ms | 18 | 61734 | 954 | 839 | $0.2060 |
