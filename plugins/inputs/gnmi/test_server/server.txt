package main

import (
	"context"
	"fmt"
	"net"
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

	// Ensure we continuously send updates for eth0 to eth9
	for {
		select {
		case <-stream.Context().Done():
			// Handle client disconnection
			fmt.Println("Stream closed or context canceled")
			return stream.Context().Err()
		default:
			// Create a notification for each interface eth0 to eth9
			var updates []*gnmi.Update
			for i := 0; i < 10; i++ {
				// Create path for eth0 to eth9 interfaces
				interfaceName := fmt.Sprintf("eth%d", i)
				update := &gnmi.Update{
					Path: &gnmi.Path{
						Elem: []*gnmi.PathElem{
							{Name: "interfaces"},
							{Name: "interface", Key: map[string]string{"name": interfaceName}},
						},
					},
					Val: &gnmi.TypedValue{
						Value: &gnmi.TypedValue_IntVal{IntVal: 10}, // Return value 10 for each interface
					},
				}
				updates = append(updates, update)
			}

			// Adding updates for additional paths like storage, hardware, and alarm
			// Adding Storage path
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
					Value: &gnmi.TypedValue_StringVal{StringVal: "500GB"}, // Example storage value
				},
			})

			// Adding Hardware path
			updates = append(updates, &gnmi.Update{
				Path: &gnmi.Path{
					Elem: []*gnmi.PathElem{
						{Name: "hardware"},
						{Name: "model"},
					},
				},
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_StringVal{StringVal: "model-XYZ"}, // Example hardware model
				},
			})

			// Adding Alarm path
			updates = append(updates, &gnmi.Update{
				Path: &gnmi.Path{
					Elem: []*gnmi.PathElem{
						{Name: "alarm"},
						{Name: "state"},
						{Name: "active"},
					},
				},
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_StringVal{StringVal: "No Alarms"}, // Example alarm status
				},
			})

			updates = append(updates, &gnmi.Update{
				Path: &gnmi.Path{
					Elem: []*gnmi.PathElem{
						{Name: "system"},
						{Name: "state"},
						{Name: "hostname"},
					},
				},
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_StringVal{StringVal: "fake_server1"}, // Example alarm status
				},
			})

			// Create a notification with all updates
			notification := &gnmi.Notification{
				Update: updates,
			}

			// Wrap the notification in a SubscribeResponse
			subscribeResponse := &gnmi.SubscribeResponse{
				Response: &gnmi.SubscribeResponse_Update{
					Update: notification, // Send the whole notification
				},
			}

			// Send the SubscribeResponse to the client
			if err := stream.Send(subscribeResponse); err != nil {
				// Log error and return if Send fails
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

func main() {
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

	// Initialize the server
	lis, err := net.Listen("tcp", ":10161")
	if err != nil {
		panic(err)
	}
	grpcServer := grpc.NewServer()
	gnmi.RegisterGNMIServer(grpcServer, &fakeGNMIServer{data: data})

	// Start the server
	fmt.Println("Fake gNMI Server is running on :10161")
	if err := grpcServer.Serve(lis); err != nil {
		panic(err)
	}
}
