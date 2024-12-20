package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	gnmi "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
)

// fakeGNMIServer represents the gNMI server
type fakeGNMIServer struct {
	gnmi.UnimplementedGNMIServer
	// Store predefined values for paths
	data map[string]string
}

// Get handles gNMI GetRequests
func (s *fakeGNMIServer) Get(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
	fmt.Println("Received Get Request:", req)
	var notifications []*gnmi.Notification

	// Call req.GetPath() to retrieve the slice of paths
	paths := req.GetPath()
	for _, path := range paths {
		// Convert path to string (simple representation)
		pathStr := pathToString(path)

		// Lookup the path in the data map
		if value, exists := s.data[pathStr]; exists {
			// Create an update for the requested path
			update := &gnmi.Update{
				Path: path,
				Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_StringVal{StringVal: value}},
			}
			// Create a notification containing the update
			notification := &gnmi.Notification{
				Update: []*gnmi.Update{update},
			}
			notifications = append(notifications, notification)
		} else {
			fmt.Printf("Path not found: %s\n", pathStr)
		}
	}

	// Construct and return the GetResponse
	response := &gnmi.GetResponse{
		Notification: notifications,
	}
	return response, nil
}

// Subscribe handles gNMI Subscribe requests (newly implemented)
func (s *fakeGNMIServer) Subscribe(stream gnmi.GNMI_SubscribeServer) error {
	fmt.Println("Received Subscribe Request")

	// Process each subscription request from the stream
	for {
		// Receive a subscription request from the client
		req, err := stream.Recv()
		if err != nil {
			fmt.Printf("Error receiving subscription request: %v\n", err)
			return err
		}

		// Extract the paths from the subscription request
		subList := req.GetSubscribe()
		if subList == nil {
			fmt.Println("Invalid subscribe request: missing Subscribe field")
			continue
		}

		// Log the requested paths
		fmt.Println("Requested subscription paths:")
		for _, subscription := range subList.Subscription {
			fmt.Println("Path:", pathToString(subscription.Path))
		}

		// Ensure we continuously send updates based on the requested paths
		for {
			select {
			case <-stream.Context().Done():
				// Handle client disconnection
				fmt.Println("Stream closed or context canceled")
				return stream.Context().Err()
			default:
				// Prepare updates based on the requested paths
				var updates []*gnmi.Update
				for _, subscription := range subList.Subscription {
					path := subscription.Path
					pathStr := pathToString(path)

					// Dynamically generate responses based on the path
					switch pathStr {
					case "/interfaces/interface":
						// Add updates for interfaces eth0 to eth9
						for i := 0; i < 10; i++ {
							interfaceName := fmt.Sprintf("eth%d", i)
							updates = append(updates, &gnmi.Update{
								Path: &gnmi.Path{
									Elem: []*gnmi.PathElem{
										{Name: "interfaces"},
										{Name: "interface", Key: map[string]string{"name": interfaceName}},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_IntVal{IntVal: 10}, // Example value
								},
							})
						}
					case "/storage/state/capacity":
						updates = append(updates, &gnmi.Update{
							Path: &gnmi.Path{
								Elem: []*gnmi.PathElem{
									{Name: "storage"},
									{Name: "state"},
									{Name: "capacity"},
									{Name: "sAvail"},
								},
							},
							Val: &gnmi.TypedValue{
								Value: &gnmi.TypedValue_StringVal{StringVal: "500GB"}, // Example value
							},
						})
					case "/hardware/model":
						updates = append(updates, &gnmi.Update{
							Path: &gnmi.Path{
								Elem: []*gnmi.PathElem{
									{Name: "hardware"},
									{Name: "model"},
								},
							},
							Val: &gnmi.TypedValue{
								Value: &gnmi.TypedValue_StringVal{StringVal: "model-XYZ"}, // Example value
							},
						})
					case "/alarm/state":
						updates = append(updates, &gnmi.Update{
							Path: &gnmi.Path{
								Elem: []*gnmi.PathElem{
									{Name: "alarm"},
									{Name: "state"},
									{Name: "active"},
								},
							},
							Val: &gnmi.TypedValue{
								Value: &gnmi.TypedValue_StringVal{StringVal: "No Alarms"}, // Example value
							},
						})
					case "/system/state/hostname":
						updates = append(updates, &gnmi.Update{
							Path: &gnmi.Path{
								Elem: []*gnmi.PathElem{
									{Name: "system"},
									{Name: "state"},
									{Name: "hostname"},
								},
							},
							Val: &gnmi.TypedValue{
								Value: &gnmi.TypedValue_StringVal{StringVal: "fake_server1"}, // Example value
							},
						})
					default:
						fmt.Printf("Unknown subscription path: %s\n", pathStr)
					}
				}

				// Create a notification with all updates
				notification := &gnmi.Notification{
					Update: updates,
				}

				// Wrap the notification in a SubscribeResponse
				subscribeResponse := &gnmi.SubscribeResponse{
					Response: &gnmi.SubscribeResponse_Update{
						Update: notification,
					},
				}

				// Send the SubscribeResponse to the client
				if err := stream.Send(subscribeResponse); err != nil {
					fmt.Printf("Error sending SubscribeResponse: %v\n", err)
					return err
				}

				// Log the sent response (debugging)
				fmt.Println("Sent SubscribeResponse to client")

				// Wait for a second before sending the next update
				time.Sleep(10 * time.Second)
			}
		}
	}
}

// pathToString converts a gNMI path to a string representation
func pathToString(path *gnmi.Path) string {
	var result string
	for _, elem := range path.Elem {
		result += "/" + elem.Name
		if len(elem.Key) > 0 {
			result += "["
			for k, v := range elem.Key {
				result += fmt.Sprintf("%s=%s,", k, v)
			}
			result = result[:len(result)-1] + "]" // Remove trailing comma and close the bracket
		}
	}
	return result
}

// Function to start the gNMI server on a given hostname and port
func startServer(hostname string, port int, wg *sync.WaitGroup) {
	defer wg.Done()

	// Predefined data for the server
	data := make(map[string]string)
	for i := 0; i < 10; i++ {
		interfaceName := fmt.Sprintf("/interfaces/interface[name=eth%d]/state/oper-status", i)
		data[interfaceName] = "UP" // Default value for all interfaces
	}

	// Adding additional paths for storage, hardware, and alarm
	data["/storage/state/capacity"] = "500GB"
	data["/hardware/model"] = "model-XYZ"
	data["/alarm/state"] = "No Alarms"
	data["/system/state/hostname"] = "fake_server1"

	// Create a listener for the specified hostname and port
	address := fmt.Sprintf("%s:%d", hostname, port)
	lis, err := net.Listen("tcp", address)
	if err != nil {
		panic(err)
	}

	// Create a new gRPC server and register the gNMI server
	grpcServer := grpc.NewServer()
	gnmi.RegisterGNMIServer(grpcServer, &fakeGNMIServer{data: data})

	// Start the server
	fmt.Printf("Fake gNMI Server is running on %s\n", address)
	if err := grpcServer.Serve(lis); err != nil {
		panic(err)
	}
}

func main() {
	var wg sync.WaitGroup

	// Hostnames simulation (localhost, localhost1, localhost2, ...)
	hostnames := []string{
		"localhost1", "localhost2", "localhost3", "localhost4",
		"localhost5", "localhost6", "localhost7", "localhost8", "localhost9",
	}

	// Start 10 servers in parallel (on different hostnames and ports)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go startServer(hostnames[i], 10161+i, &wg) // Running servers on different hostnames and ports
	}

	// Wait for all servers to finish (though this will run indefinitely)
	wg.Wait()
}
