// Transport generations revoke observations across socket replacement without
// changing RPC requests, retained signed bytes, or reconnect ownership.
package rpc

// Zero means closed or uninitialized. It never reappears as a valid owner.
func (self *Client) TransportGeneration() uint64 {
	if self == nil || self.transportGeneration == nil {
		return 0
	}
	return self.transportGeneration.Load()
}

// Called at real disconnect/reconnect boundaries. Saturation refuses future
// authority instead of wrapping into an earlier issued generation.
func (self *Client) advanceTransportGeneration() {
	if self == nil || self.transportGeneration == nil {
		return
	}
	for {
		before := self.transportGeneration.Load()
		if before == 0 || self.transportGeneration.CompareAndSwap(before, before+1) {
			return
		}
	}
}
