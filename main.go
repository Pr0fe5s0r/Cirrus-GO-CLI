package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	ipfs "github.com/ipfs/go-ipfs-api"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/libp2p/go-libp2p-core/routing"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	tls "github.com/libp2p/go-libp2p/p2p/security/tls"

	// "github.com/libp2p/go-libp2p-core/peerstore"
	peer "github.com/libp2p/go-libp2p-core/peer"
	kadht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/muxer/mplex"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	ping "github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	"github.com/multiformats/go-multiaddr"
	ma "github.com/multiformats/go-multiaddr"
)

var (
	chost host.Host
)

const (
	topicCirrus = "cirrus-hash-sharing"
	topicIPFS   = "cirrus-ipfs-sharing"
)

const storageLimitFile = "storage_limit.json"
const dataJSON = "data.json"

var limit storageLimit

type storageLimit struct {
	SLimit int64 `json:"slimit"`
	ULimit int64 `json:"ulimit"`
}

// type discoveryNotifee struct {
// 	h   host.Host
// 	ctx context.Context
// }

type PinnedObject struct {
	Hash string `json:"hash"`
}

type Data struct {
	Hash      string
	PeerID    string
	MultiAddr []string
}

type JDATA struct {
	Array []Data `json:"array"`
}

type BootstrapAddress struct {
	Address string `json:"address"`
}

// func handleStream(stream network.Stream) {
// 	fmt.Println("Got a new stream!")

// 	// Create a buffer stream for non blocking read and write.
// 	rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

// 	go readData(rw)
// 	go writeData(rw)

// 	// 'stream' will stay open until you close it (or the other side closes it).
// }

// func readData(rw *bufio.ReadWriter) {
// 	for {
// 		str, err := rw.ReadString('\n')
// 		if err != nil {
// 			fmt.Println("Error reading from buffer")
// 			panic(err)
// 		}

// 		if str == "" {
// 			return
// 		}
// 		if str != "\n" {
// 			// Green console colour: 	\x1b[32m
// 			// Reset console colour: 	\x1b[0m
// 			fmt.Printf("\x1b[32m%s\x1b[0m> ", str)
// 		}

// 	}
// }

// func writeData(rw *bufio.ReadWriter) {
// 	fmt.Println("Writing Data")
// 	for {
// 		sendData := "Hello world"
// 		var err interface{}
// 		_, err = rw.WriteString(fmt.Sprintf("%s\n", sendData))
// 		if err != nil {
// 			fmt.Println("Error writing to buffer")
// 			panic(err)
// 		}
// 		err = rw.Flush()
// 		if err != nil {
// 			fmt.Println("Error flushing buffer")
// 			panic(err)
// 		}
// 	}
// }

func checkNodeStatus(nodeUrl string) error {
	resp, err := http.Post(nodeUrl+"/api/v0/id", "", io.MultiReader())
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("Node at %s is not accessible", nodeUrl)
	}

	return nil
}

func ReadBootstrapAddressFromFile(filename string) ([]BootstrapAddress, error) {
	// Read the contents of the file into a byte slice
	contents, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	// Unmarshal the contents of the file into a slice of BootstrapAddress structs
	var addresses []BootstrapAddress
	if err := json.Unmarshal(contents, &addresses); err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %w", err)
	}

	return addresses, nil
}

func StartHTTPSever(ctx context.Context, topic *pubsub.Topic, host host.Host) {
	http.HandleFunc("/addPin", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(users)
		case http.MethodPost:
			var user hash
			err := json.NewDecoder(r.Body).Decode(&user)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			usreHash := user.Hash
			verifyHash, verr := cid.Decode(usreHash)
			if verr != nil {
				res := map[string]interface{}{
					"msg": "Sorry this is a wrong with IPFS hash",
				}
				json.NewEncoder(w).Encode(res)
			} else {
				verifiedHash := verifyHash.String()

				rres := map[string]interface{}{
					"msg":  "Pinned Hash",
					"hash": verifiedHash,
				}

				wres := map[string]interface{}{
					"msg":  "Not Pinned. Something Went Wrong.",
					"hash": verifiedHash,
				}

				// eres := map[string]interface{}{
				// 	"msg":  "The File is already pinned.",
				// 	"hash": verifiedHash,
				// }

				newShel := ipfs.NewShell("http://localhost:5001")
				err := newShel.Pin(verifiedHash)
				errw := streamConsoleTo(ctx, topic, verifiedHash, host, false)
				initialCheck(host, ctx)
				if errw != "" {
					json.NewEncoder(w).Encode(errw)
					break
				}

				if err != nil {
					json.NewEncoder(w).Encode(wres)
				} else {
					json.NewEncoder(w).Encode(rres)
				}
				break
			}
		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})

	fmt.Println("Server is running on http://localhost:8080")
	errf := http.ListenAndServe(":8085", nil)
	if errf != nil {
		fmt.Println(errf)
	}
}

func main() {
	help := flag.Bool("help", false, "Display Help")
	init := flag.Bool("init", false, "Will create a new pairof Keys")
	cfg := parseFlags()

	if *help {
		fmt.Printf("Simple example for peer discovery using mDNS. mDNS is great when you have multiple peers in local LAN.")
		fmt.Printf("Usage: \n   Run './chat-with-mdns'\nor Run './chat-with-mdns -host [host] -port [port] -rendezvous [string] -pid [proto ID]'\n")
		generateRSAKey()
		os.Exit(0)
	} else if *init {
		generateRSAKey()
	}

	_, errw := os.Stat("private.pem")
	if os.IsNotExist(errw) {
		fmt.Println("Please create new Pair of RSA keys. \n ./cirrus-cli --init")
		os.Exit(0)
	}

	err := checkNodeStatus("http://127.0.0.1:5001")

	if err != nil {
		fmt.Println("Please run your IPFS node")
		os.Exit(0)
	}

	fmt.Println("Node is accessible")
	shell := ipfs.NewShell("http://127.0.0.1:5001")
	node, err := shell.ID()

	if err != nil {
		fmt.Println("Sry Node Hash not set")
		return
	}
	fmt.Println("\n***************************This is the IPFS node INfo:*************************")
	for _, addr := range node.Addresses {
		fmt.Println(addr)
	}

	fmt.Printf("[*] Listening on: %s with port: %d\n", cfg.listenHost, cfg.listenPort)

	ctx := context.Background()
	//r := rand.Reader
	rsaKey := getRSAPrivKey()

	if rsaKey == nil {
		fmt.Println("Something is wrong with Keys. Please generate New Keys \n ./cirrus-cli --init")
	}

	// Creates a new RSA key pair for this host.
	// prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	// if err != nil {
	// 	panic(err)
	// }

	// 0.0.0.0 will listen on any interface device.
	sourceMultiAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", cfg.listenHost, cfg.listenPort))
	transports := libp2p.ChainOptions(
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(websocket.New),
	)

	muxers := libp2p.ChainOptions(
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
		libp2p.Muxer("/mplex/6.7.0", mplex.DefaultTransport),
	)

	security := libp2p.Security(tls.ID, tls.New)

	// libp2p.New constructs a new libp2p Host.
	// Other options can be added here.
	var dhtt *kadht.IpfsDHT
	newDHT := func(h host.Host) (routing.PeerRouting, error) {
		var err error
		dhtt, err = kadht.New(ctx, h)
		return dhtt, err
	}
	routing := libp2p.Routing(newDHT)
	chost, err := libp2p.New(
		libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.Identity(rsaKey),
		transports,
		muxers,
		security,
		routing,
	)

	if err != nil {
		panic(err)
	}

	ps, err := pubsub.NewGossipSub(ctx, chost)
	if err != nil {
		panic(err)
	}

	topic, err := ps.Join(topicCirrus)
	ipfstopic, err := ps.Join(topicIPFS)
	if err != nil {
		panic(err)
	}

	addresses, err := ReadBootstrapAddressFromFile("bootstrap_addresses.json")
	if err != nil {
		fmt.Println("please do cirrus-cli init", err)
		return
	}

	fmt.Println("Bootstrap Addresses:")
	for _, address := range addresses {
		arrinfo, err := peer.AddrInfoFromString(address.Address)
		if err != nil {
			fmt.Println(err)
		}
		if err := chost.Connect(ctx, *arrinfo); err != nil {
			fmt.Println("Connection failed:", err)
			continue
		}

		fmt.Println("Connected to:", arrinfo.ID)
	}
	// Set a function as stream handler.
	// This function is called when a peer initiates a connection and starts a stream with this peer.
	//host.SetStreamHandler(protocol.ID(cfg.ProtocolID), handleStream)
	go StartHTTPSever(ctx, topic, chost)
	go initialCheck(chost, ctx)
	fmt.Printf("\n[*] Your Multiaddress Is: /ip4/%s/tcp/%v/p2p/%s\n", cfg.listenHost, cfg.listenPort, chost.ID().Pretty())
	peerChan := initMDNS(chost, "CIRRUS-DISCOVERY")

	// Bootstrap the DHT.
	// if err = dht.Bootstrap(context.Background()); err != nil {
	// 	fmt.Println("Failed to bootstrap DHT:", err)
	// 	return
	// }
	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ticker.C:
				go initialCheck(chost, ctx)
				//pubsubIPFSPeer(ctx, ipfstopic)
				//Discover other peers in the network.
				for _, peer := range chost.Network().Peers() {
					if _, err := chost.Peerstore().SupportsProtocols(peer, chatProtocol); err == nil {
						s, err := chost.NewStream(ctx, peer, chatProtocol)
						defer func() {
							if err != nil {
								fmt.Fprintln(os.Stderr, err)
							}
						}()
						if err != nil {
							continue
						}
						err = chatSend(chost.ID().Pretty(), s)
					}

					if _, err := chost.Peerstore().SupportsProtocols(peer, requestConnections); err == nil {
						s, err := chost.NewStream(ctx, peer, requestConnections)
						defer func() {
							if err != nil {
								fmt.Fprintln(os.Stderr, err)
							}
						}()
						if err != nil {
							continue
						}
						peers := chost.Network().Peers()

						// Iterate over the list of peers and print their addresses
						for _, peerID := range peers {
							addr := chost.Peerstore().PeerInfo(peerID).Addrs[0]
							fmt.Printf("Sending Peer: %s\n", addr.String()+"/p2p/"+peerID.Pretty())
							err = chatSend(addr.String()+"/p2p/"+peerID.Pretty(), s)
						}
					}
				}

				for _, peer := range chost.Network().Peers() {
					if _, err := chost.Peerstore().SupportsProtocols(peer, ipfsddress); err == nil {
						s, err := chost.NewStream(ctx, peer, ipfsddress)
						defer func() {
							if err != nil {
								fmt.Fprintln(os.Stderr, err)
							}
						}()
						if err != nil {
							continue
						}

						shell := ipfs.NewShell("http://127.0.0.1:5001")
						node, err := shell.ID()

						if err != nil {
							fmt.Println("Sry Node Hash not set")
							return
						}
						for _, addr := range node.Addresses {
							//fmt.Println(addr)
							ip, err := GetIPAddress(addr)
							if err != nil {
								fmt.Println(err)
								continue
							}
							if ip != "127.0.0.1" {
								//fmt.Println(addr)
								//fmt.Printf("Sending IPFS Peer: %s\n", addr)
								err = ipfsend(addr, s)
							}
						}
					}
				}

				//fmt.Println("Bootstrap Addresses:")
				for _, address := range addresses {
					arrinfo, err := peer.AddrInfoFromString(address.Address)
					if err != nil {
						//fmt.Println(err)
						break
					}
					if err := chost.Connect(ctx, *arrinfo); err != nil {
						//fmt.Println("Connection failed:", err)
						break
					}

					fmt.Println("Connected to:", arrinfo.ID)
				}
			}
		}
	}()

	// go func() {
	// 	ticker := time.NewTicker(10 * time.Second)
	// 	for {
	// 		select {
	// 		case <-ticker.C:
	// 			initialCheck(chost, ctx)
	// 			//pubsubIPFSPeer(ctx, ipfstopic)
	// 			// Discover other peers in the network.
	// 			for _, peer := range chost.Network().Peers() {
	// 				if _, err := chost.Peerstore().SupportsProtocols(peer, chatProtocol); err == nil {
	// 					s, err := chost.NewStream(ctx, peer, chatProtocol)
	// 					defer func() {
	// 						if err != nil {
	// 							fmt.Fprintln(os.Stderr, err)
	// 						}
	// 					}()
	// 					if err != nil {
	// 						continue
	// 					}
	// 					shell := ipfs.NewShell("http://127.0.0.1:5001")
	// 					node, err := shell.ID()

	// 					if err != nil {
	// 						fmt.Println("Sry Node Hash not set")
	// 						return
	// 					}
	// 					for _, addr := range node.Addresses {
	// 						//fmt.Println(addr)
	// 						ip, err := GetIPAddress(addr)
	// 						if err != nil {
	// 							//fmt.Println(err)
	// 							continue
	// 						}
	// 						if ip != "127.0.0.1" {
	// 							//fmt.Println(addr)
	// 							//fmt.Printf("Sending IPFS Peer: %s\n", addr)
	// 							err = ipfsend(addr, s)
	// 						}
	// 					}
	// 				}

	// 				// if _, err := chost.Peerstore().SupportsProtocols(peer, requestConnections); err == nil {
	// 				// 	s, err := chost.NewStream(ctx, peer, requestConnections)
	// 				// 	defer func() {
	// 				// 		if err != nil {
	// 				// 			fmt.Fprintln(os.Stderr, err)
	// 				// 		}
	// 				// 	}()
	// 				// 	if err != nil {
	// 				// 		continue
	// 				// 	}
	// 				// 	peers := chost.Network().Peers()

	// 				// 	// Iterate over the list of peers and print their addresses
	// 				// 	for _, peerID := range peers {
	// 				// 		addr := chost.Peerstore().PeerInfo(peerID).Addrs[0]
	// 				// 		fmt.Printf("Sending Peer: %s\n", addr.String()+"/p2p/"+peerID.Pretty())
	// 				// 		err = chatSend(addr.String()+"/p2p/"+peerID.Pretty(), s)
	// 				// 	}
	// 				// }
	// 			}

	// 			for _, peer := range chost.Network().Peers() {
	// 				if _, err := chost.Peerstore().SupportsProtocols(peer, requestConnections); err == nil {
	// 					s, err := chost.NewStream(ctx, peer, requestConnections)
	// 					defer func() {
	// 						if err != nil {
	// 							fmt.Fprintln(os.Stderr, err)
	// 						}
	// 					}()
	// 					if err != nil {
	// 						continue
	// 					}

	// 					peers := chost.Network().Peers()

	// 					// Iterate over the list of peers and print their addresses
	// 					for _, peerID := range peers {
	// 						addr := chost.Peerstore().PeerInfo(peerID).Addrs[0]
	// 						ip, err := GetIPAddress(addr.String())
	// 						if err != nil {
	// 							panic(err)
	// 						}
	// 						if ip != "127.0.0.1" {
	// 							//fmt.Println(addr)
	// 							fmt.Printf("Sending Peer: %s\n", addr.String()+"/p2p/"+peerID.Pretty())
	// 							err = chatSend(addr.String()+"/p2p/"+peerID.Pretty(), s)
	// 						}
	// 					}
	// 				}
	// 			}

	// 			// for _, peer := range chost.Network().Peers() {
	// 			// 	if _, err := chost.Peerstore().SupportsProtocols(peer, ipfsddress); err == nil {
	// 			// 		s, err := chost.NewStream(ctx, peer, ipfsddress)
	// 			// 		defer func() {
	// 			// 			if err != nil {
	// 			// 				fmt.Fprintln(os.Stderr, err)
	// 			// 			}
	// 			// 		}()
	// 			// 		if err != nil {
	// 			// 			continue
	// 			// 		}

	// 			// 		shell := ipfs.NewShell("http://127.0.0.1:5001")
	// 			// 		node, err := shell.ID()

	// 			// 		if err != nil {
	// 			// 			fmt.Println("Sry Node Hash not set")
	// 			// 			return
	// 			// 		}
	// 			// 		for _, addr := range node.Addresses {
	// 			// 			fmt.Println(addr)
	// 			// 			ip, err := GetIPAddress(addr)
	// 			// 			if err != nil {
	// 			// 				fmt.Println(err)
	// 			// 				continue
	// 			// 			}
	// 			// 			if ip != "127.0.0.1" {
	// 			// 				fmt.Println(addr)
	// 			// 				//fmt.Printf("Sending IPFS Peer: %s\n", addr)
	// 			// 				err = chatSend(addr, s)
	// 			// 			}
	// 			// 		}
	// 			// 	}
	// 			// }

	// 			//fmt.Println("Bootstrap Addresses:")
	// 			for _, address := range addresses {
	// 				arrinfo, err := peer.AddrInfoFromString(address.Address)
	// 				if err != nil {
	// 					//fmt.Println(err)
	// 					break
	// 				}
	// 				if err := chost.Connect(ctx, *arrinfo); err != nil {
	// 					//fmt.Println("Connection failed:", err)
	// 					break
	// 				}

	// 				fmt.Println("Connected to:", arrinfo.ID)
	// 			}
	// 		}
	// 	}
	// }()
	chost.SetStreamHandler(chatProtocol, func(s network.Stream) {
		chatHandler(ctx, s)
	})
	chost.SetStreamHandler(requestConnections, func(stream network.Stream) {
		requestConnection(ctx, chost, stream)
	})
	chost.SetStreamHandler(ipfSHash, func(stream network.Stream) {
		ipfsHashCat(stream)
	})
	chost.SetStreamHandler(ipfsddress, func(s network.Stream) {
		ipfsSwarmConnect(ctx, s)
	})
	chost.SetStreamHandler("/cirrus/1.0.0", func(stream network.Stream) {
		//fmt.Println("New incoming stream opened")
		// Read the message from the stream.
		message, err := bufio.NewReader(stream).ReadString('\n')
		if err != nil {
			fmt.Println("Failed to read message from stream:", err)
			return
		}

		fmt.Printf("Pinning and Verifying Hash: %s", message)
		pinIPFSHash(message)
	})
	//notifee := &discoveryNotifee{h: chost, ctx: ctx}
	routingDiscovery := drouting.NewRoutingDiscovery(dhtt)

	dutil.Advertise(ctx, routingDiscovery, string("/cirrus/1.0.0"))
	peers, err := dutil.FindPeers(ctx, routingDiscovery, string("/cirrus/1.0.1"))
	if err != nil {
		panic(err)
	}
	for _, peer := range peers {
		fmt.Println(peer.ID)
	}
	//ticker := time.NewTicker(time.Second * 10)
	for { // allows multiple peers to join
		peers := <-peerChan // will block untill we discover a peer
		fmt.Println("Found peer:", peers.ID)
		if err := chost.Connect(ctx, peers); err != nil {
			fmt.Println("Connection failed:", err)
			continue
		}

		fmt.Println("Connected to:", peers.ID)
		// go streamConsoleTo(ctx, topic)
		go printMessagesFrom(ctx, topic, chost)
		go printIPFSHash(ctx, ipfstopic, chost)
		//RandomPeers(chost)
		// // open a stream, this stream will be handled by handleStream other end
		// stream, err := chost.NewStream(ctx, peer.ID, protocol.ID(cfg.ProtocolID))
		// handleStream(stream)
		// if err != nil {
		// 	fmt.Println("Stream open failed", err)
		// } else {
		// 	rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

		// 	go writeData(rw)
		// 	go readData(rw)
		// 	fmt.Println("Connected to:", peer)
		// }
	}

}

func GetIPAddress(maddrStr string) (string, error) {
	maddr, err := ma.NewMultiaddr(maddrStr)
	if err != nil {
		return "", err
	}

	ipByte, errs := maddr.ValueForProtocol(ma.P_IP4)

	if len(ipByte) == 0 {
		return "", fmt.Errorf("multiaddress %s does not contain an IP address", errs)
	}

	return ipByte, nil
}

// func RandomPeers(host host.Host) peer.ID {
// 	rand.Seed(time.Now().UnixNano())
// 	peers := host.Peerstore().PeersWithAddrs()
// 	// for _, peerInfo := range peers {
// 	// 	if peerInfo.ID == peerID {
// 	// 		fmt.Println("Multiaddress of the connected node:", peerInfo.Addrs[0].String())
// 	// 		break
// 	// 	}
// 	// }
// 	if len(peers) == 0 {
// 		fmt.Println("No connected peers.")
// 	}
// 	//fmt.Println(rand.Int())
// 	randomPeer := peers[rand.Intn(len(peers))]
// 	if randomPeer.Pretty() != host.ID().Pretty() {
// 		fmt.Println("Random connected peer:", randomPeer.Pretty())
// 		return randomPeer
// 	}
// 	fmt.Println(randomPeer.Pretty())
// 	return ""
// }

func sendStream(host host.Host, peerID peer.ID, hash string) (bool, error) {
	// Connect to the target node
	err := host.Connect(context.Background(), peer.AddrInfo{ID: peerID})
	if err != nil {
		fmt.Println("Error connecting to peer:", err)
		return false, nil
	}

	// Open a new stream to the target node
	stream, err := host.NewStream(context.Background(), peerID, protocol.ID("/cirrus-ipfs/pinhash"))
	if err != nil {
		fmt.Println("Error opening stream:", err)
		return false, nil
	}

	// Write the message to the stream
	_, err = stream.Write([]byte(hash))
	if err != nil {
		fmt.Println("Error writing to stream:", err)
		return false, nil
	}

	// Close the stream
	err = stream.Close()
	if err != nil {
		fmt.Println("Error closing stream:", err)
		return false, nil
	}

	return true, err
}

func verifyStream(host host.Host, peerID peer.ID, hash string) {
	// Connect to the target node
	err := host.Connect(context.Background(), peer.AddrInfo{ID: peerID})
	if err != nil {
		fmt.Println("Error connecting to peer:", err)
		return
	}

	// Open a new stream to the target node
	stream, err := host.NewStream(context.Background(), peerID, protocol.ID("/cirrus-ipfs/verifyhash"))
	if err != nil {
		fmt.Println("Error opening stream:", err)
		return
	}

	// Write the message to the stream
	_, err = stream.Write([]byte(hash))
	if err != nil {
		fmt.Println("Error writing to stream:", err)
		return
	}

	// Close the stream
	// err = stream.Close()
	// if err != nil {
	// 	fmt.Println("Error closing stream:", err)
	// 	return
	// }
}

func printStreamMessage(stream network.Stream) {
	buffer := make([]byte, 1024)
	n, err := stream.Read(buffer)
	if err != nil {
		fmt.Println(err)
		return
	}

	message := string(buffer[:n])
	fmt.Println("Received IPFS hash:", message)
	pinIPFSHash(message)
}

func checkIfPinned(hash string) bool {
	// Make a request to the IPFS HTTP API to retrieve the list of pinned objects
	resp, err := http.Get("http://localhost:5001/api/v0/pin/ls")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	// Read the response body into a byte slice
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	// Unmarshal the JSON response into a slice of PinnedObject structs
	var pinnedObjects []PinnedObject
	if err := json.Unmarshal(body, &pinnedObjects); err != nil {
		panic(err)
	}

	// Iterate over the slice of PinnedObject structs and check if the hash is pinned
	for _, obj := range pinnedObjects {
		if obj.Hash == hash {
			return true
		}
	}

	return false
}

func ReadDataJSON(hash string) string {
	file, err := os.OpenFile(dataJSON, os.O_RDONLY, 0644)
	if err != nil {
		fmt.Println("Error: Unale to Open the file")
		return "not"
	}
	defer file.Close()

	var data []Data
	err = json.NewDecoder(file).Decode(&data)
	if err != nil {
		fmt.Println("Error decoding JSON data:", err)
		return "not"
	}

	for _, d := range data {
		if d.Hash == hash {
			return "not"
		}
	}
	return ""
}

func GetJSONData() []Data {
	file, err := os.OpenFile(dataJSON, os.O_RDONLY, 0644)
	if err != nil {
		return nil
	}
	defer file.Close()

	var data []Data
	err = json.NewDecoder(file).Decode(&data)
	if err != nil {
		fmt.Println("GetJSONdata Error decoding JSON data:", err)
		return nil
	}

	// for _, d := range data {
	// 	if d.Hash == hash {
	// 		return ""
	// 	}
	// }
	return data
}

func streamConsoleTo(ctx context.Context, topic *pubsub.Topic, hash string, host host.Host, isRe bool) string {
	eers := ReadDataJSON(hash)
	if eers != "" && isRe != true {
		fmt.Println("Hash is already Pinned")
		return "Hash is already Pinned"
	}

	rand.Seed(time.Now().UnixNano())
	peers := host.Peerstore().PeersWithAddrs()
	// for _, peerInfo := range peers {
	// 	if peerInfo.ID == peerID {
	// 		fmt.Println("Multiaddress of the connected node:", peerInfo.Addrs[0].String())
	// 		break
	// 	}
	// }
	if len(peers) == 0 {
		fmt.Println("No connected peers.")
	}
	//fmt.Println(rand.Int())
	randomPeer := peers[rand.Intn(len(peers))]
	for host.ID().Pretty() == randomPeer.Pretty() {
		nrandomPeer := peers[rand.Intn(len(peers))]
		randomPeer = nrandomPeer
	}

	npeerID := randomPeer
	//go verifyStream(host, npeerID, "Hello. this is a request")
	// err := host.Connect(context.Background(), peer.AddrInfo{ID: npeerID})
	// if err != nil {
	// 	fmt.Println("Error connecting to peer trying:", err)
	// 	streamConsoleTo(ctx, topic, hash, host, isRe)
	// 	return ""
	// }

	allpeers := host.Network().Peers()
	var mul []ma.Multiaddr
	for _, peer := range allpeers {
		if npeerID.Pretty() == peer.Pretty() {
			addrs := host.Network().Peerstore().Addrs(peer)
			mul = addrs
			break
		}
	}

	stringAddrs := make([]string, len(mul))
	for i, multiAddr := range mul {
		stringAddrs[i] = multiAddr.String()
	}

	for {
		rw, err := startPeerAndConnect(host, npeerID, hash)
		var file *os.File
		file, err = os.OpenFile(dataJSON, os.O_RDWR, 0644)
		if rw == nil {
			fmt.Println("Unable to Send the Hash")
		}
		fmt.Println("HASH IS SENT")
		//go startPeerAndConnect(host, npeerID.Pretty(), hash)
		var storedData []Data
		if err := json.NewDecoder(file).Decode(&storedData); err != nil {
			fmt.Println("Error decoding JSON data from the data.json file:", err)
			return ""
		}

		// for _, d := range storedData {
		// 	if d.PeerID == npeerID.Pretty() && len(allpeers) > 0 {
		// 		fmt.Println("Same Peer")
		// 		streamConsoleTo(ctx, topic, hash, host, isRe)
		// 		break
		// 	}
		// }

		if isRe {
			file, err = os.OpenFile(dataJSON, os.O_RDWR, 0644)
			if err != nil {
				fmt.Println("Error opening the data.json file:", err)
				return ""
			}
			defer file.Close()

			if err := json.NewDecoder(file).Decode(&storedData); err != nil {
				fmt.Println("Error decoding JSON data from the data.json file:", err)
				return ""
			}

			for i, d := range storedData {
				if d.Hash == hash {
					storedData[i].PeerID = npeerID.Pretty()
					storedData[i].MultiAddr = stringAddrs
					break
				}
			}

			if err != nil {
				fmt.Println(err)
				return ""
			}
			os.Remove(dataJSON)
			file, errw := os.Create(dataJSON)
			if errw != nil {
				fmt.Println("Error: Unable to delete the file")
				return ""
			}
			//json.NewEncoder(file).SetIndent("", "")
			if err := json.NewEncoder(file).Encode(storedData); err != nil {
				fmt.Println("Error encoding JSON data:", err)
				return ""
			}
			return ""
		}

		if _, err := os.Stat(dataJSON); os.IsNotExist(err) {
			// If the file does not exist, create it and set the storage limit to 100 MB
			file, err := os.Create(dataJSON)
			if err != nil {
				fmt.Println(err)
				return ""
			}
			defer file.Close()

			storedData = append(storedData, Data{
				Hash:      hash,
				PeerID:    npeerID.Pretty(),
				MultiAddr: stringAddrs,
			})

			if err := json.NewEncoder(file).Encode(storedData); err != nil {
				fmt.Println("Error encoding JSON data:", err)
				return ""
			}

			file.Close()
			// os.Chmod(dataJSON, 0400)
		} else {
			// If the file exists, read the storage limit from it
			var file *os.File
			file, err = os.OpenFile(dataJSON, os.O_RDWR, 0644)
			if err != nil {
				fmt.Println("Error opening the data.json file:", err)
				return ""
			}
			defer file.Close()

			if err := json.NewDecoder(file).Decode(&storedData); err != nil {
				fmt.Println("Error decoding JSON data from the data.json file:", err)
				return ""
			}

			storedData = append(storedData, Data{
				Hash:      hash,
				PeerID:    npeerID.Pretty(),
				MultiAddr: stringAddrs,
			})

			if err != nil {
				fmt.Println(err)
				return ""
			}
			os.Remove(dataJSON)
			file, errw := os.Create(dataJSON)
			if errw != nil {
				fmt.Println("Error: Unable to delete the file")
				return ""
			}
			//json.NewEncoder(file).SetIndent("", "")
			if err := json.NewEncoder(file).Encode(storedData); err != nil {
				fmt.Println("Error encoding JSON data:", err)
				return ""
			}

		}

		return ""
	}
}

// func pubsubIPFSPeer(ctx context.Context, topic *pubsub.Topic) string {
// 	//shell := ipfs.NewShell("http://127.0.0.1:5001")
// 	//node, err := shell.ID()
// 	// if err != nil {
// 	// 	fmt.Println("Sry Node Hash not set")
// 	// 	return ""
// 	// }

// 	// for _, addr := range node.Addresses {
// 	// 	fmt.Println(addr)
// 	// 	laddr = append(laddr, addr)
// 	// }
// 	resp, err := http.Get("http://checkip.amazonaws.com")
// 	if err != nil {
// 		fmt.Println("Error retrieving IP:", err)
// 		return ""
// 	}
// 	defer resp.Body.Close()
// 	body, err := ioutil.ReadAll(resp.Body)
// 	if err != nil {
// 		fmt.Println("Error reading response:", err)
// 		return ""
// 	}

// 	// fmt.Println("Public IP:", string(body))
// 	// for {
// 	// 	if err := topic.Publish(ctx, []byte("Hello World")); err != nil {
// 	// 		fmt.Println("### Publish error:", err)
// 	// 		return ""
// 	// 	} else {
// 	// 		fmt.Println("Log: Sent Your IPFS peer ID")
// 	// 		return ""
// 	// 	}
// 	// }

// 	//return ""
// }

func printMessagesFrom(ctx context.Context, topic *pubsub.Topic, chost host.Host) {

	// if _, err := os.Stat(storageLimitFile); os.IsNotExist(err) {
	// 	// If the file does not exist, create it and set the storage limit to 100 MB
	// 	limit = storageLimit{
	// 		ULimit: 1024 * 1024 * 500,
	// 		SLimit: 1024 * 1024 * 500,
	// 	}
	// 	file, err := os.Create(storageLimitFile)
	// 	if err != nil {
	// 		fmt.Println(err)
	// 		return
	// 	}
	// 	defer file.Close()
	// 	json.NewEncoder(file).Encode(limit)
	// 	file.Close()
	// 	os.Chmod(storageLimitFile, 0400)
	// } else {
	// 	// If the file exists, read the storage limit from it
	// 	file, err := os.Open(storageLimitFile)
	// 	if err != nil {
	// 		fmt.Println(err)
	// 		return
	// 	}
	// 	defer file.Close()
	// 	json.NewDecoder(file).Decode(&limit)
	// }

	sub, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	for {
		m, err := sub.Next(ctx)
		if err != nil {
			panic(err)
		}
		thispeer := chost.ID().Pretty()
		if thispeer == m.ReceivedFrom.String() {
			continue
		} else {
			fmt.Println(m.ReceivedFrom, ": ", string(m.Message.Data))
			newShel := ipfs.NewShell("http://localhost:5001")
			data, err := newShel.Cat(string(m.Message.Data))
			if err != nil {
				fmt.Println("Error: Unable to reach the hash")
			}
			if data != nil {
				fmt.Println("IPFS hash is Pinned")
			}
		}
	}
}

func printIPFSHash(ctx context.Context, topic *pubsub.Topic, chost host.Host) {

	// if _, err := os.Stat(storageLimitFile); os.IsNotExist(err) {
	// 	// If the file does not exist, create it and set the storage limit to 100 MB
	// 	limit = storageLimit{
	// 		ULimit: 1024 * 1024 * 500,
	// 		SLimit: 1024 * 1024 * 500,
	// 	}
	// 	file, err := os.Create(storageLimitFile)
	// 	if err != nil {
	// 		fmt.Println(err)
	// 		return
	// 	}
	// 	defer file.Close()
	// 	json.NewEncoder(file).Encode(limit)
	// 	file.Close()
	// 	os.Chmod(storageLimitFile, 0400)
	// } else {
	// 	// If the file exists, read the storage limit from it
	// 	file, err := os.Open(storageLimitFile)
	// 	if err != nil {
	// 		fmt.Println(err)
	// 		return
	// 	}
	// 	defer file.Close()
	// 	json.NewDecoder(file).Decode(&limit)
	// }

	sub, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	for {
		m, err := sub.Next(ctx)
		if err != nil {
			panic(err)
		}
		thispeer := chost.ID().Pretty()
		if thispeer == m.ReceivedFrom.String() {
			continue
		} else {
			fmt.Println(m.ReceivedFrom, ": ", string(m.Message.Data))
			// newShel := ipfs.NewShell("http://localhost:5001")
			// data, err := newShel.Cat(string(m.Message.Data))
			// if err != nil {
			// 	fmt.Println("Error: Unable to reach the hash")
			// }
			// if data != nil {
			// 	fmt.Println("IPFS hash is Pinned")
			// }
		}
	}
}

func pinIPFSHash(hash string) {
	newShel := ipfs.NewShell("http://localhost:5001")
	str := strings.ReplaceAll(hash, "\n", "")
	err := newShel.Pin(str)
	if err != nil {
		fmt.Println("Error: IPFS hash is not Pinned", err)
		return
	}
	fmt.Println("Log: IPFS Hash is Pinned")
}

func checkCirrusNode(h host.Host, ctx context.Context) bool {

	data := GetJSONData()

	for _, d := range data {
		pid := ping.NewPingService(h)

		peerID, err := peer.Decode(d.PeerID)
		if err != nil {
			fmt.Println(err)
		}

		fmt.Println(peerID)
		fmt.Println("Checking if node with peer ID", peerID, "is active")

		pinger := pid.Ping(ctx, peerID)
		select {
		case <-pinger:
			fmt.Println(d.PeerID, "Node is active")
		case <-time.After(time.Second * 5):
			fmt.Println(d.PeerID, "Node is not active")
		}
	}

	return true
}

func initialCheck(h host.Host, ctx context.Context) bool {

	data := GetJSONData()

	// if data != nil {
	// 	return false
	// }

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		fmt.Println("Error: Can't able to create a PubSub")
	}
	for _, d := range data {
		if len(d.MultiAddr) == 0 {
			fmt.Println("Error connecting to target peer:", err)
			fmt.Println("Pinnning Your Hash", d.Hash, "to different node")
			topic, err := ps.Join(topicCirrus)
			if err != nil {
				fmt.Println("Error: Can't Able to create a topic Cirrus")
			}
			streamConsoleTo(ctx, topic, d.Hash, h, true)
		}
		for _, m := range d.MultiAddr {
			targetAddr, err := ma.NewMultiaddr(m + "/p2p/" + d.PeerID)
			if err != nil {
				fmt.Println("Error creating target Multiaddress:", err)
				if err != nil {
					fmt.Println("Error connecting to target peer:", err)
					fmt.Println("Pinnning Your Hash", d.Hash, "to different node")
					topic, err := ps.Join(topicCirrus)
					if err != nil {
						fmt.Println("Error: Can't Able to create a topic Cirrus")
					}
					streamConsoleTo(ctx, topic, d.Hash, h, true)
				}
			}

			// Create a peer ID from the target Multiaddress
			targetPeerID, err := peer.AddrInfoFromP2pAddr(targetAddr)
			if err != nil {
				fmt.Println("Error creating target peer ID:", err)
				fmt.Println("Pinning your IPFS hash to another node.")
				err = h.Connect(context.Background(), *targetPeerID)
				if err != nil {
					fmt.Println("Error connecting to target peer:", err)
					fmt.Println("Pinnning Your Hash", d.Hash, "to different node")
					topic, err := ps.Join(topicCirrus)
					if err != nil {
						fmt.Println("Error: Can't Able to create a topic Cirrus")
					}
					streamConsoleTo(ctx, topic, d.Hash, h, true)
				}
			}

			// Connect to the target peer
			err = h.Connect(context.Background(), *targetPeerID)
			if err != nil {
				fmt.Println("Error connecting to target peer:", err)
				fmt.Println("Pinnning Your Hash", d.Hash, "to different node")
				topic, err := ps.Join(topicCirrus)
				if err != nil {
					fmt.Println("Error: Can't Able to create a topic Cirrus")
				}
				streamConsoleTo(ctx, topic, d.Hash, h, true)
			}

			for _, peer := range h.Network().Peers() {
				if _, err := h.Peerstore().SupportsProtocols(peer, ipfSHash); err == nil {
					s, err := h.NewStream(ctx, peer, ipfSHash)
					defer func() {
						if err != nil {
							fmt.Fprintln(os.Stderr, err)
						}
					}()
					if err != nil {
						continue
					}
					err = chatSend(d.Hash, s)
				}
			}
			fmt.Println("Successfully connected to target peer:", targetPeerID)
			go startPeerAndConnect(h, targetPeerID.ID, d.Hash)
		}
	}

	return true
}

// package main

// import (
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"log"
// 	"net/http"

// 	"github.com/libp2p/go-libp2p"
// 	pubsub "github.com/libp2p/go-libp2p-pubsub"
// )

// // Message represents a message that can be published on the pubsub network.
// type Message struct {
// 	Data string `json:"data"`
// }

// func main() {
// 	// Create a new libp2p host.
// 	host, err := libp2p.New()
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer host.Close()

// 	// Create a new pubsub topic.
// 	topic := "my-topic"
// 	ps, err := pubsub.NewGossipSub(context.Background(), host)
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	// Create a channel to receive messages from the pubsub network.
// 	ch := make(chan *pubsub.SubOpt)
// 	if _, err := ps.Subscribe(topic, *<-ch); err != nil {
// 		log.Fatal(err)
// 	}

// 	// Start the REST HTTP API server.
// 	http.HandleFunc("/publish", func(w http.ResponseWriter, r *http.Request) {
// 		if r.Method != http.MethodPost {
// 			w.WriteHeader(http.StatusMethodNotAllowed)
// 			return
// 		}

// 		// Read the request body and parse the message.
// 		var msg Message
// 		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
// 			w.WriteHeader(http.StatusBadRequest)
// 			return
// 		}

// 		// Publish the message on the pubsub network.
// 		if err := ps.Publish(topic, []byte(msg.Data)); err != nil {
// 			w.WriteHeader(http.StatusInternalServerError)
// 			return
// 		}

// 		w.WriteHeader(http.StatusOK)
// 	})

// 	http.HandleFunc("/subscribe", func(w http.ResponseWriter, r *http.Request) {
// 		if r.Method != http.MethodGet {
// 			w.WriteHeader(http.StatusMethodNotAllowed)
// 			return
// 		}

// 		// Receive messages from the pubsub network.
// 		for msg := range ch {
// 			fmt.Fprintln(w, string(msg))
// 		}
// 	})

// 	log.Fatal(http.ListenAndServe(":8081", nil))
// }
