package extensions

import (
	"encoding/json"
	"errors"
)

func cloneRaw(in json.RawMessage) json.RawMessage {
	if in == nil {
		return nil
	}
	out := make(json.RawMessage, len(in))
	copy(out, in)
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneRecord(in ToolRecord) ToolRecord {
	in.InputSchema = cloneRaw(in.InputSchema)
	in.OutputSchema = cloneRaw(in.OutputSchema)
	in.Permissions = cloneStrings(in.Permissions)
	in.Network.Allow = cloneStrings(in.Network.Allow)
	return in
}

func clonePin(p Pin) Pin {
	out := Pin{RunID: p.RunID, TenantID: p.TenantID, Tools: map[string]string{}}
	for id, version := range p.Tools {
		out.Tools[id] = version
	}
	return out
}

func cloneInv(inv Invocation) Invocation {
	inv.Input = cloneRaw(inv.Input)
	inv.Output = cloneRaw(inv.Output)
	if inv.Error != nil {
		cp := *inv.Error
		inv.Error = &cp
	}
	if inv.History != nil {
		inv.History = append([]Attempt(nil), inv.History...)
	}
	return inv
}

func errorAs(err error, target any) bool {
	return errors.As(err, target)
}
