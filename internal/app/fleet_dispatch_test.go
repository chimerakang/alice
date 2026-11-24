package app

import (
	"errors"
	"testing"
)

func TestParseFleetDispatch(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    fleetDispatchArgs
		wantErr error
	}{
		{"plain goal", "修好登入測試", fleetDispatchArgs{Goal: "修好登入測試"}, nil},
		{"goal keeps inner spacing and lines", "fix  the login\n\n- step one\n- step two", fleetDispatchArgs{Goal: "fix  the login\n\n- step one\n- step two"}, nil},
		{"issue", "#42", fleetDispatchArgs{Issue: 42}, nil},
		{"issue with surrounding space", "  #7  ", fleetDispatchArgs{Issue: 7}, nil},
		{"issue reference inside a goal is a goal", "look at #42 again", fleetDispatchArgs{Goal: "look at #42 again"}, nil},
		{"issue zero is not an issue", "#0", fleetDispatchArgs{}, errFleetDispatchEmpty},
		{"hash word is a goal", "#abc", fleetDispatchArgs{Goal: "#abc"}, nil},
		{"model before", "--model opus fix it", fleetDispatchArgs{Goal: "fix it", Model: "opus"}, nil},
		{"model after", "fix it --model opus", fleetDispatchArgs{Goal: "fix it", Model: "opus"}, nil},
		{"model in the middle", "fix --model opus it", fleetDispatchArgs{Goal: "fix  it", Model: "opus"}, nil},
		{"model with equals", "fix it --model=claude-opus-4-7", fleetDispatchArgs{Goal: "fix it", Model: "claude-opus-4-7"}, nil},
		{"budget plain", "fix it --budget 5", fleetDispatchArgs{Goal: "fix it", BudgetUSD: 5}, nil},
		{"budget dollar", "fix it --budget $2.50", fleetDispatchArgs{Goal: "fix it", BudgetUSD: 2.5}, nil},
		{"budget usd suffix", "fix it --budget=3USD", fleetDispatchArgs{Goal: "fix it", BudgetUSD: 3}, nil},
		{"both flags with an issue", "--budget 4 #12 --model sonnet", fleetDispatchArgs{Issue: 12, Model: "sonnet", BudgetUSD: 4}, nil},
		{"flags on their own line", "fix it\n--model opus\n--budget 1", fleetDispatchArgs{Goal: "fix it", Model: "opus", BudgetUSD: 1}, nil},
		{"other double-dash words stay in the goal", "add a --verbose flag", fleetDispatchArgs{Goal: "add a --verbose flag"}, nil},
		{"lookalike flag stays in the goal", "document --models and --budgets", fleetDispatchArgs{Goal: "document --models and --budgets"}, nil},
		{"empty", "", fleetDispatchArgs{}, errFleetDispatchEmpty},
		{"blank", "  \n ", fleetDispatchArgs{}, errFleetDispatchEmpty},
		{"only flags", "--model opus", fleetDispatchArgs{}, errFleetDispatchEmpty},
		{"model without a value", "fix it --model", fleetDispatchArgs{}, errFleetDispatchFlagValue},
		{"model with an empty value", "fix it --model=", fleetDispatchArgs{}, errFleetDispatchFlagValue},
		{"model swallowing a flag", "fix it --model --budget 3", fleetDispatchArgs{}, errFleetDispatchFlagValue},
		{"budget not a number", "fix it --budget lots", fleetDispatchArgs{}, errFleetDispatchBudget},
		{"budget zero", "fix it --budget 0", fleetDispatchArgs{}, errFleetDispatchBudget},
		{"budget negative", "fix it --budget -3", fleetDispatchArgs{}, errFleetDispatchBudget},
		{"model twice", "fix --model a --model b", fleetDispatchArgs{}, errFleetDispatchDuplicated},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFleetDispatch(c.in)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
