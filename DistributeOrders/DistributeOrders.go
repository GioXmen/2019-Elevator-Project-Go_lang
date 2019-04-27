package DistributeOrders

/* The DistributeOrders module take in state information of all elevators and uses hall_request_assigner to calculate
Which elevator should take which order. It access the hall_request_assigner by using terminal commands.
The information is sent as a JSON version of AllStates (defined in ElevState). Only the relevant information
for the FSM is sent out of this module. For this implementation only the local elevators orders and state is
relevant for the FSM
*/

import (
	"../ElevState"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
)

//Makes a struct type that sends the orders and state of the local elevator to the FSM
type OrderUpdate struct {
	DistributedOrders [][2]bool
	State             ElevState.SingleStates
}

var ID string //Peer ID (IP address)

//A function that distribute orders based on the hall_request_assigner.
//Takes in all elevators states and all hall request and return which elevator should take which order
//Uses redistribute all orders approach
func DistributeOrders(CalculatedOrders chan<- OrderUpdate, UpdatedAllStates <-chan ElevState.AllStates) {
	for {
		select {

		case states := <-UpdatedAllStates:  // Gets AllStates input over a channel from the ElevState module
			ElevState.Mtx.Lock()

			//first we translate our data from struct to JSON
			JSONStates, err := json.Marshal(states)
			JSONStatesString := string(JSONStates)
			ElevState.Mtx.Unlock()
			//Checks for error in converting to JSON
			if err != nil {
				fmt.Println("Error in JSON Marshal", err)
			}

			//Uses command to send the JSON to the hall_request_assigner and saves the return
			cmd := exec.Command("./hall_request_assigner", "-i", JSONStatesString)

			//Makes buffers for the order data and std error check
			var extractedAssignments bytes.Buffer
			var stderr bytes.Buffer

			//Assign the buffers
			cmd.Stdout = &extractedAssignments
			cmd.Stderr = &stderr

			//Runs the command and checks for errors
			e := cmd.Run()
			if e != nil {
				log.Panic(fmt.Sprint(e) + ": " + stderr.String())
			}

			//Make a map for all the orders and translate from JSON to this map
			orderMap := new(map[string][][2]bool)

			err = json.Unmarshal(extractedAssignments.Bytes(), orderMap)
			if err != nil {
				fmt.Println("Error in JSON UnMarshal", err)
			}

			ElevState.Mtx.Lock()
			//Transfer what the Unmarshal information point to, to a new variable to avoid pointer type problem
			orderToUse := *orderMap

			//Extract the orders and state for the local elevator, since the FSM only need the local elevator information
			res := OrderUpdate{
				DistributedOrders: orderToUse[ID],
				State:             states.States[ID],
			}
			ElevState.Mtx.Unlock()

			//Sends the OrderUpdate struct to FSM over channel
			CalculatedOrders <- res
		}
	}
}
