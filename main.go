package main

/*The main function is the entry point of the system consisting of 5 main modules: Main, FSM, ElevState, DistributeOrders
and Network. The communication between the modules is indicated where the channels are created, however, an
overview will be given here: ElevState sends the orders and state of all the elevators to DistributedOrders, which
then calculates the optimal order distribution for this elevator. DistributeOrders then sends the calculated orders
to FSM which stores them. When an event occurs, FSM sends the event type and what action was taken to ElevState. Upon
receiving this update, ElevState communicates this to the Network module, which passes it on to all the other elevators.
When receiving the update from an elevator's Network module, the receiving Network modules sends the updated states
to their ElevState modules which stores them.*/

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"./DistributeOrders"

	"./driver/elevio"

	"./FSM"
	"./Network"
	"./Network/network/localip"
	"./Network/network/peers"

	"./ElevState"
)

const NFLOORS = 4 // number of floors

var ID string   //Peer ID (IP address)
var PORT string //IP address PORT number

func main() {

	//Flags used to set the ID and PORT
	flag.StringVar(&ID, "ID", "", "The ID of this peer")                          	  //OPTIONAL: give a custom ID and/or port arguments when running. Example: run go main.go -ID=123 -PORT=456
	flag.StringVar(&PORT, "PORT", "15657", "The PORT used in connection with server") //if no arguments are given the program runs with the values given in the code
	flag.Parse()

	if ID == "" { //checks if the ID is empty and if it is assigns the localIP and process ID to it
		localIP, err := localip.LocalIP()
		if err != nil {
			fmt.Println(err)
			localIP = "DISCONNECTED"
		}
		ID = fmt.Sprintf("peer-%s", localIP)
	}
	fmt.Println(ID)

	assignGlobalVars() //Assign global variables (NFLOORS,ID,PORT) to the different modules

	PeerState := make(chan ElevState.NetworkMessage)                //Makes peer state from Network ---> ElevState channel
	UpdatedPeers := make(chan peers.PeerUpdate)                     //Makes Network peer list ---> ElevState channel
	FSMEventMsg := make(chan ElevState.EventMessage, 10)            //Makes the FSM ---> ElevState channel, with a buffer of 10 values
	UpdatedAllStates := make(chan ElevState.AllStates)              //Makes the ElevState ---> DistributeOrders channel
	MsgToNetwork := make(chan ElevState.NetworkMessage)             //Makes the updated message from ElevState ---> Network channel
	CalculatedHallOrders := make(chan DistributeOrders.OrderUpdate) //Makes the DistributeOrders ---> FSM channel

	elevio.Init("localhost:"+PORT, NFLOORS) //Inits the elev_io module and connects to the elevator server

	//Assign all the channels to their respective functions
	go ElevState.UpdateFromNetwork(PeerState, UpdatedAllStates)
	go ElevState.UpdatePeers(UpdatedPeers, UpdatedAllStates)
	go ElevState.UpdateFromFSM(FSMEventMsg, MsgToNetwork, UpdatedAllStates)
	go ElevState.UpdateOrders(UpdatedAllStates, MsgToNetwork)

	go restartProgram()

	go Network.Network(PeerState, UpdatedPeers, MsgToNetwork, ID)
	go FSM.FSM(CalculatedHallOrders, FSMEventMsg)
	go DistributeOrders.DistributeOrders(CalculatedHallOrders, UpdatedAllStates)

	ElevState.InitElevState() //Inits the ElevState module

	//Gives the FSM time to run its initialization
	t := time.Now()
	for time.Now().Sub(t) < 8*time.Second {
		select {
		case <-FSMEventMsg:
		default:
		}
	}
	select { //Empty select to keep main function running until termination
	}

}

//Restarts the elevator program if it crashes, for example: CTRL+C in the terminal window
func restartProgram() {
	sigchan := make(chan os.Signal, 10)
	signal.Notify(sigchan, os.Interrupt)
	<-sigchan
	elevio.SetMotorDirection(elevio.MD_Stop)
	log.Println("Restarting", "sh", "-c", "go run main.go")                         		 //Setting PORT and ID variables
	err := exec.Command("gnome-terminal", "-x", "sh", "-c", "go run main.go").Run() //Execute the command
	if err != nil { //Print error if the restart fails
		fmt.Println("Unable to restart")
	}
	log.Println("Program killed")
	os.Exit(0)
}

//Assign the global variables to the modules
func assignGlobalVars() {
	ElevState.NFLOORS = NFLOORS
	ElevState.ID = ID

	FSM.NFLOORS = NFLOORS
	FSM.ID = ID

	DistributeOrders.ID = ID
}
