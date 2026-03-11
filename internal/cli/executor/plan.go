package executor

import "github.com/iw2rmb/shiva/internal/cli/request"

type DispatchMode string

const DispatchModeShiva DispatchMode = "shiva"

type DispatchPlan struct {
	Mode    DispatchMode `json:"mode"`
	DryRun  bool         `json:"dry_run"`
	Network bool         `json:"network"`
}

type CallPlan struct {
	Request  request.Envelope `json:"request"`
	Dispatch DispatchPlan     `json:"dispatch"`
}

func PlanShivaCall(input request.Envelope) (CallPlan, error) {
	envelope, err := request.NormalizeResolvedCallEnvelope(input, request.DefaultShivaTarget)
	if err != nil {
		return CallPlan{}, err
	}

	return CallPlan{
		Request: envelope,
		Dispatch: DispatchPlan{
			Mode:    DispatchModeShiva,
			DryRun:  envelope.DryRun,
			Network: false,
		},
	}, nil
}
