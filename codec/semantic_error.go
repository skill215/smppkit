// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-11-06

package codec

import (
	"fmt"

	"github.com/skill215/smppkit/internal/wire"
)

// SemanticRule is a stable constraint identifier, independent of error prose.
// The set is open: callers must tolerate new rules. Empty means unspecified.
type SemanticRule string

const (
	RuleValue       SemanticRule = "value"       // assigned values or reserved bits
	RuleLength      SemanticRule = "length"      // version/definition byte bound
	RuleCount       SemanticRule = "count"       // collection bound
	RuleUnused      SemanticRule = "unused"      // field or inactive union arm must be empty/zero
	RuleVersion     SemanticRule = "version"     // unavailable in the selected version
	RuleCommand     SemanticRule = "command"     // unavailable for this command/profile
	RuleDirection   SemanticRule = "direction"   // unavailable in the request's direction
	RuleConflict    SemanticRule = "conflict"    // mutually exclusive representations
	RuleDependency  SemanticRule = "dependency"  // related fields missing or inconsistent
	RuleCardinality SemanticRule = "cardinality" // prohibited repeated TLV
	RuleTime        SemanticRule = "time"        // protocol date/time grammar or components
	RuleMode        SemanticRule = "mode"        // incompatible messaging mode
	RuleStatus      SemanticRule = "status"      // incompatible command/response status
	RuleProfile     SemanticRule = "profile"     // advertised interface version mismatches Codec
	RuleBody        SemanticRule = "body"        // missing/wrong typed body or illegal omission
	RuleExtension   SemanticRule = "extension"   // customer validator rejected; rule itself is opaque
)

// SemanticError identifies a built-in validation failure without exposing values.
// Field and Rule are stable identifiers documented in docs/semantic-diagnostics.md.
// Error text is not an API. New identifiers may be added in minor versions.
//
// Index is zero-based within dest_address, unsuccess_sme or the complete ordered
// Optional slice, only when HasIndex is true. Tag is valid only when HasTag is
// true, including tag zero. Collection-wide failures have no index. No wire
// offset is invented for typed input; a historical FieldError remains in the
// cause where applicable. DecodeError classifications are unchanged.
//
// Fields are diagnostic snapshots. Do not mutate a shared error concurrently.
// Unwrap preserves the previous cause(s); custom causes can contain sensitive
// data even though this type's Error method never formats them.
type SemanticError struct {
	Field    string
	Rule     SemanticRule
	Index    int
	HasIndex bool
	Tag      uint16
	HasTag   bool
	cause    error
}

// Error formats only diagnostic identity and location, never cause text.
func (e *SemanticError) Error() string {
	return fmt.Sprintf("smpp: semantic field=%s rule=%s indexed=%t index=%d tagged=%t tag=0x%04x", e.Field, e.Rule, e.HasIndex, e.Index, e.HasTag, e.Tag)
}

// Unwrap exposes prior causes; arbitrary callback causes may contain values.
func (e *SemanticError) Unwrap() error { return e.cause }

func invalidField(field string, rule SemanticRule) *SemanticError {
	return &SemanticError{Field: field, Rule: rule, cause: ErrValidation}
}

func unsupportedField(field string, rule SemanticRule) *SemanticError {
	return &SemanticError{Field: field, Rule: rule, cause: ErrUnsupported}
}

func invalidTLV(tag uint16, index int, rule SemanticRule) *SemanticError {
	e := invalidField("optional", rule)
	e.Tag, e.HasTag = tag, true
	if index >= 0 {
		e.Index, e.HasIndex = index, true
	}
	return e
}

// locateSemantic decorates only errors constructed here, never custom errors.
// Copy before adding context so no previously published snapshot is modified.
func locateSemantic(err error, field string, index int) error {
	if e, ok := err.(*SemanticError); ok {
		copy := *e
		copy.Field, copy.Index, copy.HasIndex = field, index, true
		return &copy
	}
	return err
}

// failSemantic preserves historical wire.Error fields/offsets and DecodeMandatory
// for semantic checks that originally used Reader.Fail. Earlier read errors win.
func failSemantic(r *wire.Reader, field string, rule SemanticRule, reason string) {
	if r.Err == nil {
		r.Fail(field, reason)
		e := invalidField(field, rule)
		e.cause = r.Err
		r.Err = e
	}
}
