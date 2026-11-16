package outbound

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"

	"github.com/metacubex/mihomo/log"
	oc "github.com/sagernet/sing-openconnect"
)

const openConnectPacketQueueLength = 32

type openConnectStack struct {
	device    ipStack
	addresses []netip.Prefix
	mtu       uint32
}

func (s *openConnectStack) matches(config oc.TunnelConfiguration) bool {
	return s != nil && s.mtu == config.MTU && slices.Equal(s.addresses, config.Addresses)
}

func (o *OpenConnect) start() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return o.err
	}
	if o.started {
		return nil
	}
	if err := o.client.Start(); err != nil {
		return err
	}
	o.started = true
	o.workers.Add(2)
	go o.receivePackets()
	go o.authenticate()
	return nil
}

func (o *OpenConnect) readyStack(ctx context.Context) (*openConnectStack, error) {
	if err := o.start(); err != nil {
		return nil, err
	}
	for {
		if err := o.client.WaitReady(ctx); err != nil {
			o.mu.Lock()
			if o.err != nil {
				err = o.err
			}
			o.mu.Unlock()
			return nil, err
		}
		o.mu.Lock()
		s, err := o.configureStackLocked()
		o.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if s != nil {
			return s, nil
		}
	}
}

func (o *OpenConnect) configurationChanged(_ oc.TunnelConfigurationEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil
	}
	// Events are asynchronous; use the current snapshot, never a queued old event.
	_, err := o.configureStackLocked()
	return err
}

func (o *OpenConnect) configureStackLocked() (*openConnectStack, error) {
	if o.closed {
		return nil, o.err
	}
	if !o.client.Ready() {
		return nil, nil
	}
	config := o.client.TunnelConfiguration()
	o.updatePolicyLocked(config)
	if o.stack.matches(config) {
		return o.stack, nil
	}
	if config.MTU < 576 || config.MTU > 65535 || len(config.Addresses) == 0 {
		return nil, errors.New("openconnect server supplied an invalid tunnel address or MTU")
	}
	for _, prefix := range config.Addresses {
		if !prefix.IsValid() || prefix.Addr().IsUnspecified() || prefix.Addr().IsMulticast() {
			return nil, errors.New("openconnect server supplied an invalid tunnel address")
		}
	}
	device, err := newIPStack(o.option.IPStack, config.Addresses, config.MTU)
	if err != nil {
		return nil, err
	}
	if err = device.Start(); err != nil {
		_ = device.Close()
		return nil, err
	}
	old := o.stack
	s := &openConnectStack{device: device, addresses: slices.Clone(config.Addresses), mtu: config.MTU}
	o.stack = s
	if old != nil {
		_ = old.device.Close()
		// The library's receive queue spans sessions. Discard its bounded backlog
		// before new sockets can receive packets, including MTU-only replacements.
		drainCtx, cancel := context.WithCancel(o.ctx)
		cancel()
		for i := 0; i < openConnectPacketQueueLength; i++ {
			if _, err := o.client.ReadDataPacket(drainCtx); err != nil {
				break
			}
		}
		log.Infoln("[OpenConnect](%s) tunnel address or MTU changed; previous connections closed", o.Name())
	}
	o.workers.Add(1)
	go o.sendPackets(s)
	return s, nil
}

func (o *OpenConnect) receivePackets() {
	defer o.workers.Done()
	for {
		s, err := o.readyStack(o.ctx)
		if err != nil {
			o.stop(err)
			return
		}
		packet, err := o.client.ReadDataPacket(o.ctx)
		if err != nil {
			o.stop(err)
			return
		}
		o.mu.Lock()
		current, err := o.configureStackLocked()
		// A read may span a reconnect. Never inject its packet into a replacement stack.
		if err == nil && s == current && openConnectPacketAddress(packet, s.addresses, false) {
			_, err = s.device.Write([][]byte{packet}, 0)
		}
		o.mu.Unlock()
		if err != nil {
			o.stop(err)
			return
		}
	}
}

func (o *OpenConnect) sendPackets(s *openConnectStack) {
	defer o.workers.Done()
	buffers := make([][]byte, s.device.BatchSize())
	sizes := make([]int, len(buffers))
	for i := range buffers {
		buffers[i] = make([]byte, 65535)
	}
	for {
		n, err := s.device.Read(buffers, sizes, 0)
		if err != nil {
			o.mu.Lock()
			current := o.stack == s && !o.closed
			o.mu.Unlock()
			if current {
				o.stop(err)
			}
			return
		}
		for i := 0; i < n; i++ {
			packet := buffers[i][:sizes[i]]
			o.mu.Lock()
			current := o.stack == s && !o.closed && o.client.Ready() && s.matches(o.client.TunnelConfiguration())
			o.mu.Unlock()
			if !current {
				continue
			}
			if !openConnectPacketAddress(packet, s.addresses, true) {
				continue
			}
			// The library copies the packet and bounds its queue. On transport failure it
			// owns reconnection; dropping here lets TCP retransmit without a second retry loop.
			if err := o.client.WriteDataPacket(packet); err != nil && o.ctx.Err() != nil {
				return
			}
		}
	}
}

func openConnectPacketAddress(packet []byte, addresses []netip.Prefix, source bool) bool {
	if len(packet) < 20 {
		return false
	}
	var address netip.Addr
	switch packet[0] >> 4 {
	case 4:
		offset := 16
		if source {
			offset = 12
		}
		address = netip.AddrFrom4([4]byte(packet[offset : offset+4]))
	case 6:
		if len(packet) < 40 {
			return false
		}
		offset := 24
		if source {
			offset = 8
		}
		address = netip.AddrFrom16([16]byte(packet[offset : offset+16]))
	default:
		return false
	}
	for _, prefix := range addresses {
		if prefix.Addr() == address {
			return true
		}
	}
	return false
}

func (o *OpenConnect) stop(err error) {
	o.mu.Lock()
	if !o.closed {
		o.closed = true
		o.err = err
		if o.err == nil {
			o.err = net.ErrClosed
		}
		o.cancel()
		if o.stack != nil {
			_ = o.stack.device.Close()
			o.stack = nil
		}
	}
	o.mu.Unlock()
	_ = o.client.Close()
}
