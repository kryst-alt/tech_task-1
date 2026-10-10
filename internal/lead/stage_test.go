package lead

import "testing"

func TestCanMoveTo(t *testing.T) {
	cases := []struct {
		from, to Stage
		want     bool
	}{
		{StageNew, StageQualified, true},
		{StageNew, StageLost, true},
		{StageNew, StageProposal, false},
		{StageQualified, StageProposal, true},
		{StageQualified, StageNew, false},
		{StageProposal, StageWon, true},
		{StageProposal, StageLost, true},
		{StageWon, StageLost, false},
		{StageLost, StageNew, false},
		{StageNew, StageNew, false},
	}
	for _, c := range cases {
		if got := c.from.CanMoveTo(c.to); got != c.want {
			t.Errorf("%s -> %s: got %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestParseStage(t *testing.T) {
	if st, err := ParseStage("proposal"); err != nil || st != StageProposal {
		t.Fatalf("ParseStage(proposal) = %q, %v", st, err)
	}
	if _, err := ParseStage("Proposal"); err == nil {
		t.Fatal("ParseStage(Proposal) should fail, stages are lowercase")
	}
}
