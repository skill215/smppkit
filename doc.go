// Package smpp implements general-purpose SMPP endpoints. A Session owns one
// connection generation; reconnecting requires a new session and never replays
// requests. Submission acceptance is separate from final message delivery.
package smpp
