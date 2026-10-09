package runtime_v1

import "iter"

// FrameIterator yields the encoded answers of one already decoded request.
//
// The caller ranges over the iterator. Each pair is one frame: the encoded
// bytes and an error. A nil error means the caller writes the bytes and keeps
// the connection. See [Runtime.Handle] for [errs.ErrCloseConnection] and
// [errs.ErrLogAndIgnore]. A batch yields one frame per nested command as it
// finishes, or one combined frame when the batch asks for a single answer.
// false from the yield function stops the iterator. Parallel workers finish
// before the iterator returns.
type FrameIterator = iter.Seq2[[]byte, error]

// YieldFrameIterator is the yield function passed to a [FrameIterator].
// false means the caller stopped ranging and the iterator must return.
type YieldFrameIterator = func([]byte, error) bool
