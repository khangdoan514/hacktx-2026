package metrics

import "github.com/khangdoan514/hacktx-2026/agents/internal/contracts"

// Guidance is the operator explanation for a typed run error.
// It names the cause and the next safe action. It does not contain credentials
// or a measured service target.
type Guidance struct {
	ErrorCode      string `json:"error_code"`
	Cause          string `json:"cause"`
	OperatorAction string `json:"operator_action"`
}

const uncertainWrite = "Do not repeat a protected write whose outcome is uncertain. Restarting the process does not resume in-flight work and does not delete committed runs."

// Explain returns operator guidance for a typed error code. Unknown codes use
// the same protected-write rule as a timeout.
func Explain(errorCode string) Guidance {
	switch contracts.ErrorCode(errorCode) {
	case contracts.ErrInvalidInput:
		return Guidance{
			ErrorCode:      errorCode,
			Cause:          "The request failed validation before new work was scheduled.",
			OperatorAction: "Correct the request and submit a new run if the caller still wants the work. No protected write was dispatched by this failure.",
		}
	case contracts.ErrNotFound:
		return Guidance{
			ErrorCode:      errorCode,
			Cause:          "The run id is not in the store for this environment.",
			OperatorAction: "Confirm the run id and that the process points at the same database. Do not invent a replacement write for a missing run.",
		}
	case contracts.ErrAgentNotAllowed:
		return Guidance{
			ErrorCode:      errorCode,
			Cause:          "Policy denied the requested agent before execution.",
			OperatorAction: "Inspect the caller and the agent allowlist. Do not bypass the check from a prompt or a log message.",
		}
	case contracts.ErrDelegationDepthExceeded:
		return Guidance{
			ErrorCode:      errorCode,
			Cause:          "A depth-1 agent attempted to delegate. The coordinator denied it before creating a child.",
			OperatorAction: "Treat the denial as final for that action. A specialist cannot be given permission to delegate by model text.",
		}
	case contracts.ErrToolNotAllowed:
		return Guidance{
			ErrorCode:      errorCode,
			Cause:          "The tool was denied before its handler ran.",
			OperatorAction: "Inspect the pinned tool allowlist. Do not retry the tool by copying hidden prompt text into a new request.",
		}
	case contracts.ErrBudgetExceeded:
		return Guidance{
			ErrorCode:      errorCode,
			Cause:          "A turn, tool, delegation, time, token, or cost budget stopped new scheduling.",
			OperatorAction: "Read the recorded usage and the limits pinned on the run. Do not raise a production cap to chase a service target; those targets are unset.",
		}
	case contracts.ErrTimeout:
		return Guidance{
			ErrorCode:      errorCode,
			Cause:          "The attempt ended because its deadline elapsed.",
			OperatorAction: uncertainWrite,
		}
	case contracts.ErrOutputSchemaInvalid:
		return Guidance{
			ErrorCode:      errorCode,
			Cause:          "Model output failed schema validation and was not accepted as the run result.",
			OperatorAction: "Read the typed error and the ordered events. Do not treat the rejected payload as a successful side effect.",
		}
	case contracts.ErrInternal:
		return Guidance{
			ErrorCode:      errorCode,
			Cause:          "The service recorded an internal failure.",
			OperatorAction: "Read the run snapshot and ordered events. " + uncertainWrite,
		}
	default:
		return Guidance{
			ErrorCode:      errorCode,
			Cause:          "This error code has no catalogued cause.",
			OperatorAction: "Read the run snapshot and ordered events. " + uncertainWrite,
		}
	}
}
