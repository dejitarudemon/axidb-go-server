package server

// writeLoop drains encoded answers for one connection until the context ends.
func (s *Server) writeLoop(ctx connContext) {
	for {
		select {
		case <-ctx.Done():
			return
		case answer := <-ctx.answers:
			if len(answer) == 0 {
				continue
			}

			if _, err := ctx.writer.Write(answer); err != nil {
				s.error("failed to answer", "error", err, "source", peer(ctx))
				s.closeConn(ctx)
				return
			}

			if err := ctx.writer.Flush(); err != nil {
				s.error("failed to flush answer", "error", err, "source", peer(ctx))
				s.closeConn(ctx)
				return
			}
		}
	}
}

// sendAnswer enqueues answer for [Server.writeLoop].
// A nil or empty answer is ignored. A cancelled context drops the answer.
func (s *Server) sendAnswer(ctx connContext, answer []byte) {
	if len(answer) == 0 {
		return
	}
	if ctx.Err() != nil {
		return
	}

	select {
	case <-ctx.Done():
	case ctx.answers <- answer:
	}
}
