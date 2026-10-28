// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package pdu

import (
	"bytes"
	"slices"

	"github.com/skill215/smppkit/tlv"
)

// DestinationKind is submit_multi's dest_flag. The discriminator is explicit:
// an empty SME address is not confused with an empty distribution-list name.
type DestinationKind uint8

const (
	SMEAddress       DestinationKind = 1
	DistributionList DestinationKind = 2
)

// MultiDestination contains exactly the arm selected by Kind. Codec rejects
// populated fields in the other arm rather than silently dropping them.
type MultiDestination struct {
	Kind     DestinationKind
	Address  Address
	ListName string
}

// SubmitMulti submits the same encoded content to a list of SME addresses and/or
// named distribution lists. Counts are derived from Destinations. This operation
// is distinct from concatenation: it neither segments text nor expands lists.
// The explicit fields avoid inheriting an unused single Destination from SMFields.
type SubmitMulti struct {
	ServiceType                                                        string
	Source                                                             Address
	Destinations                                                       []MultiDestination
	ESMClass, ProtocolID, PriorityFlag                                 uint8
	ScheduleDeliveryTime, ValidityPeriod                               string
	RegisteredDelivery, ReplaceIfPresent, DataCoding, DefaultMessageID uint8
	ShortMessage                                                       []byte
	Optional                                                           tlv.Fields
}

func (*SubmitMulti) CommandID() CommandID { return SubmitMultiID }

// Clone deep-copies all mutable data, including destination and TLV slices.
func (b SubmitMulti) Clone() SubmitMulti {
	b.Destinations = slices.Clone(b.Destinations)
	b.ShortMessage = bytes.Clone(b.ShortMessage)
	b.Optional = b.Optional.Clone()
	return b
}

// UnsuccessfulSME retains a destination and its individual submission status.
// Status is opaque: the library does not infer retryability or final delivery.
type UnsuccessfulSME struct {
	Address Address
	Status  Status
}

// SubmitMultiResp can have command_status OK while Unsuccessful is nonempty.
// Always inspect both the PDU status and this list. Absence from the failure list
// means submission acceptance only; distribution-list membership is peer owned.
// Optional is supported in 5.0 only. An omitted negative body uses Omitted.
type SubmitMultiResp struct {
	MessageID    string
	Unsuccessful []UnsuccessfulSME
	Optional     tlv.Fields
}

func (*SubmitMultiResp) CommandID() CommandID { return SubmitMultiRespID }

// Clone deep-copies the per-SME failure list and optional parameter values.
func (b SubmitMultiResp) Clone() SubmitMultiResp {
	b.Unsuccessful = slices.Clone(b.Unsuccessful)
	b.Optional = b.Optional.Clone()
	return b
}
