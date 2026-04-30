# Spike #149 Results

Model: `claude-sonnet-4-5-20250929`  Workdir: `/private/tmp/spike-target`  Sub-tasks: 5


## Per-mode totals

| Mode | Calls | Cost (reported) | Cost (5m-norm) | Total input | cache_read | cache_write (1h share) | Output | Wallclock |
|---|---|---|---|---|---|---|---|---|
| walking | 5 | $0.7059 | $0.2037 | 295,253 | 275,887 (93.4%) | 19,276 (0 @1h) | 3,222 | 73.6s |

## Per-turn detail


### walking

| # | Sub-task | Duration | Input | cache_read | cache_write | Output | Cost |
|---|---|---|---|---|---|---|---|
| 1 | List all top-level files and directories in the current work | 17601ms | 18 | 39688 | 15821 | 597 | $0.0802 |
| 2 | Read the file you saw named CLAUDE.md (or README.md if CLAUD | 13690ms | 18 | 56476 | 1011 | 601 | $0.1100 |
| 3 | Search for any TODO or FIXME comments in *.go files. Report  | 15758ms | 18 | 58305 | 829 | 733 | $0.1417 |
| 4 | Run `git log --oneline -5` and report the most recent commit | 11538ms | 18 | 59969 | 766 | 508 | $0.1702 |
| 5 | Cross-reference your previous answers: was the file you read | 15058ms | 18 | 61449 | 849 | 783 | $0.2037 |
