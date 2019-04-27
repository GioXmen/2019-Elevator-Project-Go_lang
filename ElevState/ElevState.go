package ElevState

/* The ElevState handles everything that has to do with changes in the local data over every elevator sate
hall requests and cab requests. It gets it's information from the Network, the FSM and from buttons pressed.
It sends information to the DistributeOrders and Network modules. It also sets hall request and cab request lights
and define most of the own defined struct's in this program.
*/

import (
	"../Network/network/peers"
	"../driver/elevio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

//Type used to send information from the FSM to the ElevState
type EventMessage struct {
	EventType string
	//"ClearOrder"
	//"ReachFloor"
	//"StartsDriving"
	//"Stops"
	//"MotorStopsWorking"
	//"MotorWorksAgain"
	Floor               int
	Behavior            string
	Direction           string
	ClearOrderDirection string //up, down, noHall
}

//Type used to send messages between ElevState and the Network
type NetworkMessage struct {
	ID          string
	MessageType string
	//case "StateUpdate"
	//case "ClearOrder"
	//case "MotorProblems"
	//case "MotorWorksAgain"
	RemoteState         SingleStates
	HallRequests        [][2]bool
	ClearOrderDirection string
}

//Type that contains the state information and cab request for one elevator
type SingleStates struct {
	Behavior    string `json:"behaviour"`
	Floor       int    `json:"floor"`
	Direction   string `json:"direction"`
	CabRequests []bool `json:"cabRequests"`
}

//Type that contains states for all elevators on the network and hall requests
type AllStates struct {
	HallRequests [][2]bool               `json:"hallRequests"` // n x 2 matrix, n = number of floors
	States       map[string]SingleStates `json:"states"`       //states of elevator with string id
}

//Declaration of all variables that is used by more than one function in this module
var NFLOORS int
var ID string
var LocalAllStates AllStates
var InitNew SingleStates
var ThisNetworkMessage NetworkMessage

//Declare a mutex that we use to lock when operating on the share variables
var Mtx = sync.Mutex{}

func InitElevState() {
	//Inits some of the different shared variables that we use in a "standard factory" condition
	InitNew = SingleStates{Behavior: "idle", Floor: 0, Direction: "up", CabRequests: make([]bool, NFLOORS)}
	LocalAllStates = AllStates{HallRequests: make([][2]bool, NFLOORS), States: make(map[string]SingleStates)}
	LocalAllStates.States[ID] = InitNew
	ThisNetworkMessage = NetworkMessage{ID: ID, MessageType: "", RemoteState: InitNew, HallRequests: make([][2]bool, NFLOORS)} //Should make init function for this

	//if statement that checks if it starts a new elevator, or recovers on program "crash"
	if _, err := os.Stat("elevator_states.txt"); err == nil { //if the file exists, load it into LocalAllStates
		fmt.Println("TRYING TO OPEN FILE")
		data, err := os.Open("elevator_states.txt") //open the files
		check(err)
		defer data.Close() //makes sure it will be closed

		tmp := new(AllStates) //makes a tmp to save the loaded data to

		fileHandle := json.NewDecoder(data).Decode(tmp) //load file content into tmp

		*tmp = changeStateInAllStates(*tmp, ID, "stop", 0, "idle") //Hall-orders and cab orders the same, rest initialized

		LocalAllStates = *tmp //Transfer the data to LocalAllStates
		check(fileHandle)
		fmt.Println("Loaded LocalAllStates from file")
		SetLights(LocalAllStates, ID)
		fmt.Println("Finished ElevState INIT")

	} else { //if the file doesn't exist, create the file and initialize LocalAllStates
		fmt.Println("TRYING TO CREAT FILE")
		file, error := os.Create("elevator_states.txt") //Makes a new file
		check(error)

		defer file.Close() //make sure it will be closed
		tmp := LocalAllStates
		savingFile(tmp, ID) //Saves the LocalAllStates to the file
		fmt.Println("Finished ElevState INIT")

	}

}

//Function that handles updates from the network module
func UpdateFromNetwork(PeerState <-chan NetworkMessage, UpdatedAllStates chan<- AllStates) {
	for {
		select {
		case networkData := <-PeerState: //Receives peer data from the network
			receivedID := networkData.ID

			//Copy the share AllStates (LocalAllStates) variable to a one that is only used locally in this func (networkAllStates)
			networkAllStates := copyAllState(LocalAllStates)

			if receivedID != ID { //Only change data when it is not from it self to avoid outdated data
				//Updates the state in networkAllStates variable for the received state, and adds any new hall requests
				networkAllStates = updateAllStatesNetwork(networkData, networkAllStates)

				switch TypeOfMessage := networkData.MessageType; TypeOfMessage { //checks what type of message it is

				case "StateUpdate": //in this case it doesn't need to do anything new

				case "ClearOrder": //find the floor and direction the peer tells should be cleared
					ClearFloor := networkData.RemoteState.Floor
					ClearDirection := networkData.ClearOrderDirection
					//Clears the order from hall requests in the networkAllStates variable
					networkAllStates = clearFloorOrders(networkAllStates, ClearDirection, ClearFloor, receivedID)
				}
				//Saves to file, LocalALlStates, sets elevator lights, and sends the update to DistributeOrders
				savingFile(networkAllStates, ID)
				LocalAllStates = networkAllStates
				SetLights(networkAllStates, ID)
				UpdatedAllStates <- networkAllStates //send the updated AllStates to DistributedOrders

			}
		}
	}
}

//Function that deletes peers from LocalAllStates if connection is lost/timmed out
func UpdatePeers(UpdatedPeers <-chan peers.PeerUpdate, UpdatedAllStates chan<- AllStates) {
	for {
		select {
		case peers := <-UpdatedPeers:
			for _, l := range peers.Lost { //checks the lost slice
				if l != "" && l != ID { //deletes the lost peer form LocalAllStates
					Mtx.Lock()
					delete(LocalAllStates.States, l) //Delete the lost peers
					Mtx.Unlock()

					//if alone it needs to send information to DistributeOrders to redistribute order to itself
					//When there is more than one this will happen automatically because the frequent NetworkMessages  -- Our solution to single elevator operation
					if len(peers.Peers) == 1 {
						UpdatedAllStates <- LocalAllStates
					}

				}
			}
		}
	}
}

//Function that updates the LocalAllStates based on events in in the FSM
func UpdateFromFSM(FSMEventMsg <-chan EventMessage, MsgToNetwork chan<- NetworkMessage, UpdatedAllStates chan<- AllStates) {
	for {
		select {
		case message := <-FSMEventMsg: //Event message from FSM
			Event := message.EventType // to check what FSM event has happened
			//Copy the share AllStates (LocalAllStates) varaible to a one that is only used locally in this func (fsmAllStates)
			fsmAllStates := copyAllState(LocalAllStates)

			switch Event {
			case "ClearOrder": //If the elevator has completed an order
				//Make variables for floor and direction it should clear
				floorToClear := message.Floor
				directionToClear := message.ClearOrderDirection
				//Set the Hall Request  to false on the given floor in the direction the elevator drives
				fsmAllStates = clearFloorOrders(fsmAllStates, directionToClear, floorToClear, ID)
				//Clear Cab Request for this elevator
				fsmAllStates = changeStateInAllStates(fsmAllStates, ID, message.Direction, message.Floor, message.Behavior)
				//And the updates to the Network Message

				ThisNetworkMessage.MessageType = "ClearOrder"
				ThisNetworkMessage.RemoteState = fsmAllStates.States[ID]
				ThisNetworkMessage.HallRequests = fsmAllStates.HallRequests
				ThisNetworkMessage.ClearOrderDirection = message.ClearOrderDirection

			case "ReachedNewFloor": //If it reaches a new floor but doesn't stop for order
				//Updates the local elevators state in fsmAllStates
				fsmAllStates = changeStateInAllStates(fsmAllStates, ID, message.Direction, message.Floor, message.Behavior)
				//Update the Network Message
				ThisNetworkMessage.MessageType = "StateUpdate"
				ThisNetworkMessage.RemoteState = fsmAllStates.States[ID]

			case "StartsDriving": //when it starts driving from a floor
				//Updates the local elevators state in fsmAllStates
				fsmAllStates = changeStateInAllStates(fsmAllStates, ID, message.Direction, fsmAllStates.States[ID].Floor, message.Behavior)
				//Update the Network Message
				ThisNetworkMessage.MessageType = "StateUpdate"
				ThisNetworkMessage.RemoteState = fsmAllStates.States[ID]

			case "Stops": //When the elevator stops at a floor
				//Updates the local elevators state in fsmAllStates
				fsmAllStates = changeStateInAllStates(fsmAllStates, ID, message.Direction, fsmAllStates.States[ID].Floor, message.Behavior)
				//Update the Network Message
				ThisNetworkMessage.MessageType = "StateUpdate"
				ThisNetworkMessage.RemoteState = fsmAllStates.States[ID]

			case "MotorProblems": //When the elevators motor is not working
				//Updates the local elevators state in fsmAllStates
				fsmAllStates = changeStateInAllStates(fsmAllStates, ID, message.Direction, message.Floor, message.Behavior)
				//Update the Network Message
				ThisNetworkMessage.MessageType = "MotorProblems"
				ThisNetworkMessage.RemoteState = fsmAllStates.States[ID]

			case "MotorWorksAgain": //When the elevator has reached a point where it know the motor is working again
				//Updates the local elevators state in fsmAllStates
				fsmAllStates = changeStateInAllStates(fsmAllStates, ID, message.Direction, message.Floor, message.Behavior)
				//Update the Network Message
				ThisNetworkMessage.MessageType = "MotorWorksAgain"
				ThisNetworkMessage.RemoteState = fsmAllStates.States[ID]
			}
			if len(fsmAllStates.States) == 1 { //Sets lights after FSM event if it is the only elevator on network
				SetLights(fsmAllStates, ID)
			}
			//Saves to file, LocalALlStates, and sends the update to DistributeOrders and Network
			savingFile(fsmAllStates, ID)
			LocalAllStates = fsmAllStates
			MsgToNetwork <- ThisNetworkMessage
			UpdatedAllStates <- fsmAllStates
		}
	}
}

//Updates the order when a button is pressed
func UpdateOrders(UpdatedAllStates chan<- AllStates, MsgToNetwork chan<- NetworkMessage) {
	//Inits the channel for receiving button information and a AllStates variable
	buttonAllStates := AllStates{}
	buttonPressed := make(chan elevio.ButtonEvent)
	go elevio.PollButtons(buttonPressed)

	for {
		select {
		case NewOrderLocal := <-buttonPressed: //When a button is pressed
			//Copy the share AllStates (LocalAllStates) varaible to a one that is only used locally in this func (networkAllStates)
			buttonAllStates = copyAllState(LocalAllStates)
			//Check what type of button was pressed
			switch ButtonType := NewOrderLocal.Button; ButtonType {
			case 0: //up, Sets hall request up for right floor to true
				buttonAllStates.HallRequests[NewOrderLocal.Floor][0] = true
			case 1: //down, Sets hall request down for right floor to true
				buttonAllStates.HallRequests[NewOrderLocal.Floor][1] = true
			case 2: //cab, Sets cab request for right floor to true
				buttonAllStates.States[ID].CabRequests[NewOrderLocal.Floor] = true
			}

			//Saves to file, LocalALlStates, and sends the update to DistributeOrders and Network
			ThisNetworkMessage.MessageType = "StateUpdate" //"This elevator has had an update in its state!"
			ThisNetworkMessage.HallRequests = buttonAllStates.HallRequests
			ThisNetworkMessage.RemoteState = buttonAllStates.States[ID]

			if len(buttonAllStates.States) == 1 { //Sets lights after FSM event if it is the only elevator on network -- Single elevator operation
				SetLights(buttonAllStates, ID)
			}

			savingFile(buttonAllStates, ID)
			LocalAllStates = buttonAllStates
			MsgToNetwork <- ThisNetworkMessage
			UpdatedAllStates <- buttonAllStates

		}
	}
}

func check(e error) { //check for file error
	if e != nil {
		panic(e)
	}
}

// Updatees  the local AllStates when getting information from Network
func updateAllStatesNetwork(statesFromNetwork NetworkMessage, currentAllStates AllStates) AllStates {
	receivedID := statesFromNetwork.ID // retrieved the received ID

	for peer := range currentAllStates.States { //Loop through all peers in the AllStates variable
		if peer == receivedID { //Find the one that matches the receivedID
			currentAllStates.States[receivedID] = statesFromNetwork.RemoteState //Update that elevators states
			//Checks for hall requests and set the local ones to true if the received ones were true

			for floor := range statesFromNetwork.HallRequests {
				if statesFromNetwork.HallRequests[floor][0] { //clear hall request up - 0
					currentAllStates.HallRequests[floor][0] = true
				}
				if statesFromNetwork.HallRequests[floor][1] { ////clear hall request down - 1
					currentAllStates.HallRequests[floor][1] = true
				}
			}
		}
	}

	SetLights(currentAllStates, ID) //Set the lights of the elevators
	return currentAllStates         //returns the updated AllStates
}

//Sets elevator lights based on hall requests and cab requests
func SetLights(states AllStates, id string) {
	for floor := 0; floor < NFLOORS; floor++ { //loop through and checks all
		elevio.SetButtonLamp(elevio.BT_Cab, floor, states.States[id].CabRequests[floor])
		elevio.SetButtonLamp(elevio.BT_HallUp, floor, states.HallRequests[floor][elevio.BT_HallUp])
		elevio.SetButtonLamp(elevio.BT_HallDown, floor, states.HallRequests[floor][elevio.BT_HallDown])
	}

}

// function that saves the states and hall requests of the elevator
func savingFile(states AllStates, ID string) {
	file, err := os.Create("elevator_states.txt") //Creates file that will only contain latest data
	//checks for errors and saves to file as JSON
	check(err)
	e := json.NewEncoder(file).Encode(states) //saves the AllStates struct to file
	check(e)
}

//function that will change one of the states in AllStates
func changeStateInAllStates(states AllStates, id string, dir string, floor int, behavior string) AllStates {
	//makes temp that can have changes
	tmp := states.States[id]
	tmp.Direction = dir
	tmp.Behavior = behavior
	tmp.Floor = floor

	//change the old state with the new one and return AllStates
	states.States[id] = tmp
	return states
}

//Clear orders depending on the order direction
func clearFloorOrders(state AllStates, direction string, floor int, id string) AllStates {
	if direction == "up" { //up - 0
		state.HallRequests[floor][0] = false
	}
	if direction == "down" { //down - 1
		state.HallRequests[floor][1] = false
	}
	state.States[id].CabRequests[floor] = false
	return state
}

//copies the content of an AllState type and returns it
func copyAllState(original AllStates) AllStates {
	//makes a temporary AllStates and extract the values from the input into it
	new := AllStates{}
	new.HallRequests = original.HallRequests
	new.States = make(map[string]SingleStates)

	for key, state := range original.States { // range through all the states and add them to the new AllStates
		new.States[key] = state
	}

	return new //return the new AllStates
}
