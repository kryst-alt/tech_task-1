package lead

import "fmt"

// Stage values match the lead_stage enum in Postgres.
type Stage string

const (
	StageNew       Stage = "new"
	StageQualified Stage = "qualified"
	StageProposal  Stage = "proposal"
	StageWon       Stage = "won"
	StageLost      Stage = "lost"
)

var transitions = map[Stage][]Stage{
	StageNew:       {StageQualified, StageLost},
	StageQualified: {StageProposal, StageLost},
	StageProposal:  {StageWon, StageLost},
	StageWon:       nil,
	StageLost:      nil,
}

func ParseStage(name string) (Stage, error) {
	stage := Stage(name)
	if _, ok := transitions[stage]; !ok {
		return "", fmt.Errorf("unknown stage %q", name)
	}
	return stage, nil
}

func (s Stage) CanMoveTo(next Stage) bool {
	for _, allowed := range transitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}
