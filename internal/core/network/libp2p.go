package network

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	libp2p "github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	mdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	ma "github.com/multiformats/go-multiaddr"
)

// Libp2pOptions configures the libp2p transport.
type Libp2pOptions struct {
	ListenAddrs     []string
	Bootstrap       []string
	Rendezvous      string
	EnableMDNS      bool
	IdentityKeyFile string
}

// Libp2pPubSub provides gossip-based pubsub over libp2p.
type Libp2pPubSub struct {
	ctx    context.Context
	cancel context.CancelFunc

	host host.Host
	ps   *pubsub.PubSub

	mu     sync.Mutex
	topics map[string]*pubsub.Topic
}

func NewLibp2pPubSub(parent context.Context, opts Libp2pOptions) (*Libp2pPubSub, error) {
	ctx, cancel := context.WithCancel(parent)

	listenAddrs := make([]ma.Multiaddr, 0, len(opts.ListenAddrs))
	for _, s := range opts.ListenAddrs {
		if s == "" {
			continue
		}
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("invalid listen multiaddr %q: %w", s, err)
		}
		listenAddrs = append(listenAddrs, a)
	}
	if len(listenAddrs) == 0 {
		a, _ := ma.NewMultiaddr("/ip4/0.0.0.0/tcp/0")
		listenAddrs = append(listenAddrs, a)
	}

	libp2pOpts := []libp2p.Option{libp2p.ListenAddrs(listenAddrs...)}
	if opts.IdentityKeyFile != "" {
		key, err := loadOrCreateIdentityKey(opts.IdentityKeyFile)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("load identity key: %w", err)
		}
		libp2pOpts = append(libp2pOpts, libp2p.Identity(key))
	}

	h, err := libp2p.New(libp2pOpts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create host: %w", err)
	}

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		_ = h.Close()
		cancel()
		return nil, fmt.Errorf("create gossipsub: %w", err)
	}

	p := &Libp2pPubSub{
		ctx:    ctx,
		cancel: cancel,
		host:   h,
		ps:     ps,
		topics: make(map[string]*pubsub.Topic),
	}

	if opts.EnableMDNS {
		service := mdns.NewMdnsService(h, opts.Rendezvous, &mdnsNotifee{host: h})
		if err := service.Start(); err != nil {
			log.Printf("mdns start error: %v", err)
		}
	}

	for _, raw := range opts.Bootstrap {
		if raw == "" {
			continue
		}
		addr, err := ma.NewMultiaddr(raw)
		if err != nil {
			log.Printf("skip bootstrap addr %q: %v", raw, err)
			continue
		}
		info, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			log.Printf("skip bootstrap addr %q: %v", raw, err)
			continue
		}
		if err := h.Connect(ctx, *info); err != nil {
			log.Printf("bootstrap connect failed %s: %v", info.ID, err)
		} else {
			log.Printf("connected bootstrap peer %s", info.ID)
		}
	}

	return p, nil
}

func (p *Libp2pPubSub) Publish(topic string, payload []byte) error {
	t, err := p.getOrJoinTopic(topic)
	if err != nil {
		return err
	}
	return t.Publish(p.ctx, payload)
}

func (p *Libp2pPubSub) Subscribe(topic string) (<-chan Message, func(), error) {
	t, err := p.getOrJoinTopic(topic)
	if err != nil {
		return nil, nil, err
	}
	sub, err := t.Subscribe()
	if err != nil {
		return nil, nil, err
	}

	out := make(chan Message, 64)
	subCtx, subCancel := context.WithCancel(p.ctx)
	go func() {
		defer close(out)
		for {
			msg, err := sub.Next(subCtx)
			if err != nil {
				return
			}
			select {
			case out <- Message{Topic: topic, Payload: append([]byte(nil), msg.Data...)}:
			default:
			}
		}
	}()

	cancel := func() {
		subCancel()
		sub.Cancel()
	}
	return out, cancel, nil
}

func (p *Libp2pPubSub) Close() error {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.topics {
		_ = t.Close()
	}
	return p.host.Close()
}

func (p *Libp2pPubSub) PeerID() string {
	return p.host.ID().String()
}

func (p *Libp2pPubSub) ListenAddrs() []string {
	out := make([]string, 0, len(p.host.Addrs()))
	for _, addr := range p.host.Addrs() {
		out = append(out, fmt.Sprintf("%s/p2p/%s", addr.String(), p.host.ID().String()))
	}
	return out
}

func (p *Libp2pPubSub) ConnectedPeers() []string {
	peers := p.host.Network().Peers()
	out := make([]string, 0, len(peers))
	for _, pid := range peers {
		out = append(out, pid.String())
	}
	return out
}

func (p *Libp2pPubSub) ConnectedPeerAddrs() []string {
	peers := p.host.Network().Peers()
	seen := make(map[string]struct{}, 16)
	out := make([]string, 0, len(peers))
	for _, pid := range peers {
		for _, addr := range p.host.Peerstore().Addrs(pid) {
			full := fmt.Sprintf("%s/p2p/%s", addr.String(), pid.String())
			if _, ok := seen[full]; ok {
				continue
			}
			seen[full] = struct{}{}
			out = append(out, full)
		}
	}
	return out
}

func (p *Libp2pPubSub) getOrJoinTopic(name string) (*pubsub.Topic, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.topics[name]; ok {
		return t, nil
	}
	t, err := p.ps.Join(name)
	if err != nil {
		return nil, err
	}
	p.topics[name] = t
	return t, nil
}

type mdnsNotifee struct {
	host host.Host
}

func (n *mdnsNotifee) HandlePeerFound(info peer.AddrInfo) {
	if err := n.host.Connect(context.Background(), info); err != nil {
		log.Printf("mdns connect failed %s: %v", info.ID, err)
	}
}

func loadOrCreateIdentityKey(path string) (crypto.PrivKey, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		key, err := crypto.UnmarshalPrivateKey(b)
		if err != nil {
			return nil, fmt.Errorf("unmarshal private key: %w", err)
		}
		return key, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir key dir: %w", err)
	}
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	raw, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}
	return key, nil
}
