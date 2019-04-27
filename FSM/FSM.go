package FSM

/* The FSM module acts on the current orders received from the DistributeOrders module and sends what it did
to the ElevState button. It receives an OrderUpdate type (defined in DistributeOrders) containing this elevator's
orders and its current state. It sends out messages of type EventMessage (defined in ElevState) to ElevState informing
it of changes made to the elevator states and/or orders.*/

import (
	"fmt"
	"time"

	"../DistributeOrders"
	"../ElevState"
	"../driver/elevio"
)

var ID string   // number of floors
var NFLOORS int //Peer ID (IP address)

func FSM(CalculatedOrders <-chan DistributeOrders.OrderUpdate, FSMEventMsg chan<- ElevState.EventMessage) {

	var lastUpdateMessage DistributeOrders.OrderUpdate //Message from DistributeOrders: This elevator's calculated orders and its state
	var updateMessage ElevState.EventMessage           //Message to ElevateState: What event happened(floor reached, door open, etc.) and what action was performed(motor stopping, an order was cleared, etc)
	var prevFloor = 0                                  //Previous floor of the elevator, always initialized to 0 in this implementation

	doorOpenChooseDirection := time.NewTimer(3 * time.Second) //Door is defined to be open for 3 seconds at a time
	doorOpenChooseDirection.Stop()                            //stops the timer from sending
	motorStopsWorking := time.NewTimer(5 * time.Second)
	motorStopsWorking.Stop()

	FloorSensor := make(chan int)

	go elevio.PollFloorSensor(FloorSensor)

	initializeFSM(FSMEventMsg, updateMessage, FloorSensor) /*initializing the elevator by driving it to 0th floor
	and sending an EventMsg to ElevState in order to make it start processing existing/incoming orders*/

	fmt.Println("Finished FSM INIT")

	// main FSM loop
	for {
		select {

		case newFloor := <-FloorSensor: //When a new floor is reached
			if newFloor-prevFloor > 0 {
				lastUpdateMessage.State.Direction = "up"		//Determine the direction the elevator had before reaching the floor
			} else if newFloor-prevFloor < 0 {
				lastUpdateMessage.State.Direction = "down"
			}
			prevFloor = newFloor
			elevio.SetFloorIndicator(newFloor)
			openDoorCase := false //The elevator did NOT have the door open when this event happened(it was moving when it reached the floor)

			if shouldStop(lastUpdateMessage, newFloor, openDoorCase) {
				motorStopsWorking.Stop()

				elevio.SetMotorDirection(elevio.MD_Stop)
				elevio.SetDoorOpenLamp(true)

				doorOpenChooseDirection.Reset(3 * time.Second) //Starts the "doorOpenChooseDirection.C" case after 3 seconds

				//Determine which order should be cleared and send direction to update
				if lastUpdateMessage.DistributedOrders[newFloor][0] && (lastUpdateMessage.State.Direction == "up" || !evaluateBelowOrders(lastUpdateMessage, newFloor)) {
					updateMessage.ClearOrderDirection = "up"
					updateMessage.Direction = "up"
				} else if lastUpdateMessage.DistributedOrders[newFloor][1] && (lastUpdateMessage.State.Direction == "down" || !evaluateAboveOrders(lastUpdateMessage, newFloor)) {
					updateMessage.ClearOrderDirection = "down"
					updateMessage.Direction = "down"
				} else {
					updateMessage.ClearOrderDirection = "noHall"
					updateMessage.Direction = "stop"
				}

				updateMessage.EventType = "ClearOrder"
				updateMessage.Behavior = "doorOpen"
				updateMessage.Floor = newFloor

			} else { //If elevator reaches new floor, but does not need to stop at it
				motorStopsWorking.Reset(5 * time.Second)
				updateMessage.EventType = "ReachNewFloor"
				updateMessage.Behavior = lastUpdateMessage.State.Behavior
				updateMessage.Direction = lastUpdateMessage.State.Direction
				updateMessage.Floor = newFloor

			}
			//send updated state to ElevState
			FSMEventMsg <- updateMessage

		case localElev := <-CalculatedOrders: //When receiving this elevator's state and hall orders from DistributeOrders
			lastUpdateMessage = localElev
			switch localElev.State.Behavior {
			case "idle":
				direction := chooseDirection(localElev, localElev.State.Floor)
				switch direction {
				case elevio.MD_Up:
					motorStopsWorking.Reset(5 * time.Second)
					elevio.SetMotorDirection(direction)

					updateMessage.EventType = "StartsDriving"
					updateMessage.Behavior = "moving"
					updateMessage.Direction = "up"			//Sets the direction,resets the Motor stop-timer and sends the changes to ElevState
					FSMEventMsg <- updateMessage

				case elevio.MD_Down:
					motorStopsWorking.Reset(5 * time.Second)
					elevio.SetMotorDirection(direction)

					updateMessage.EventType = "StartsDriving"
					updateMessage.Behavior = "moving"
					updateMessage.Direction = "down"
					FSMEventMsg <- updateMessage

				case elevio.MD_Stop:
					currentFloor := localElev.State.Floor

					//Checks if any new order is at the floor it currently is at
					if localElev.DistributedOrders[currentFloor][0] || localElev.DistributedOrders[currentFloor][1] || localElev.State.CabRequests[currentFloor] {
						//If so it resets the door timer and turn on lights
						elevio.SetDoorOpenLamp(true)
						doorOpenChooseDirection.Reset(3 * time.Second)

						//Clear order if there is one at this floor
						if lastUpdateMessage.DistributedOrders[currentFloor][0] {
							updateMessage.ClearOrderDirection = "up"
						} else if lastUpdateMessage.DistributedOrders[currentFloor][1] {
							updateMessage.ClearOrderDirection = "down"
						} else {
							updateMessage.ClearOrderDirection = "noHall"
						}

						updateMessage.EventType = "ClearOrder"
						updateMessage.Behavior = "doorOpen"
						updateMessage.Direction = "stop"
						FSMEventMsg <- updateMessage
					}

				}

			case "doorOpen":
				openDoorCase := true //The elevator DID have the door open when this event happened
				if shouldStop(localElev, localElev.State.Floor, openDoorCase) {
					openAtFloor := localElev.State.Floor
					elevio.SetDoorOpenLamp(true)
					doorOpenChooseDirection.Reset(3 * time.Second)

					//Clear order if there is one at this floor
					if localElev.DistributedOrders[openAtFloor][0] && (localElev.State.Direction == "up" || !evaluateBelowOrders(localElev, openAtFloor)) {
						updateMessage.ClearOrderDirection = "up"
					} else if localElev.DistributedOrders[openAtFloor][1] && (localElev.State.Direction == "down" || !evaluateAboveOrders(localElev, openAtFloor)) {
						updateMessage.ClearOrderDirection = "down"
					} else {
						updateMessage.ClearOrderDirection = "noHall"
					}

					updateMessage.EventType = "ClearOrder"
					updateMessage.Behavior = "doorOpen"
					updateMessage.Floor = openAtFloor
					FSMEventMsg <- updateMessage
				}

			case "moving":
				//If the elevator is moving it can't physically do anything with the received orders: Do nothing

			}

		case <-doorOpenChooseDirection.C: //door closes and new direction is evaluated,it is started when the 3 second timer runs out
			elevio.SetDoorOpenLamp(false)
			newDirection := chooseDirection(lastUpdateMessage, lastUpdateMessage.State.Floor) //Choosing direction based on last message from DistributeOrders

			switch newDirection {

			case elevio.MD_Stop:
				updateMessage.EventType = "Stops"
				updateMessage.Behavior = "idle"
				updateMessage.Direction = "stop"

			case elevio.MD_Up:
				motorStopsWorking.Reset(4 * time.Second)
				elevio.SetMotorDirection(newDirection)

				updateMessage.EventType = "StartsDriving"
				updateMessage.Direction = "up"
				updateMessage.Behavior = "moving"

			case elevio.MD_Down:
				motorStopsWorking.Reset(4 * time.Second)
				elevio.SetMotorDirection(newDirection)

				updateMessage.EventType = "StartsDriving"
				updateMessage.Direction = "down"
				updateMessage.Behavior = "moving"

			}
			FSMEventMsg <- updateMessage


		case <-motorStopsWorking.C: //Motor stopped working,it is started when the 5 second timer runs out

			updateMessage.EventType = "MotorProblems"
			updateMessage.Direction = lastUpdateMessage.State.Direction
			updateMessage.Behavior = "idle"
			updateMessage.Floor = lastUpdateMessage.State.Floor
			updateMessage.ClearOrderDirection = "noHall"

			FSMEventMsg <- updateMessage

			motorStopsWorking.Reset(5 * time.Second) //Keep restarting the timer as long as the motor is not working
			motorStopsWorking.Stop()

			if lastUpdateMessage.State.Direction == "down" {
				elevio.SetMotorDirection(elevio.MD_Up)
			} else {										//Set the direction the opposite of the direction it was going
				elevio.SetMotorDirection(elevio.MD_Down)	//as a safety measure in case of obstruction in the path
			}

			//Run the elevator until it reaches a floor
		MB:
			for {
				select {
				case atFloor := <-FloorSensor:
					updateMessage.EventType = "MotorWorksAgain"
					updateMessage.Direction = "stop"
					updateMessage.Behavior = "idle"
					updateMessage.Floor = atFloor
					updateMessage.ClearOrderDirection = "noHall"
					FSMEventMsg <- updateMessage //Sends update to ElevState that the motor is working again
					break MB
				}
			}



		}
	}
}

//Initializing the elevator by sending it to floor 0 and sending the updated states to ElevState
func initializeFSM(FSMEventMsg chan<- ElevState.EventMessage, updateMessage ElevState.EventMessage, FloorSensor <-chan int) {

	elevio.SetMotorDirection(elevio.MD_Down)
	elevio.SetDoorOpenLamp(false)
	updateMessage.Behavior = "idle"
	updateMessage.Direction = "up"
	updateMessage.ClearOrderDirection = "noHall"
B:
	for {
		select {
		case initFloor := <-FloorSensor:
			if initFloor == 0 {
				elevio.SetMotorDirection(elevio.MD_Stop)
				elevio.SetFloorIndicator(initFloor)
				updateMessage.Floor = initFloor
				break B
			}
		}

	}
	FSMEventMsg <- updateMessage /*Pokes the FSMEventMsg channel to make ElevState start taking orders or
	start serving recovered orders from file*/
}

/*When detecting when the elevator has reached a floor, determines if the elev
should stop based on current state + orders sent from DistributeOrders*/
func shouldStop(elevState DistributeOrders.OrderUpdate, newFloor int, doorOpenStop bool) bool {
	switch elevState.State.Direction {
	case "stop":
		fallthrough
	case "down":
		return elevState.DistributedOrders[newFloor][1] || elevState.State.CabRequests[newFloor] || (!evaluateBelowOrders(elevState, newFloor) && !doorOpenStop)
	case "up":
		return elevState.DistributedOrders[newFloor][0] || elevState.State.CabRequests[newFloor] || (!evaluateAboveOrders(elevState, newFloor) && !doorOpenStop)
	default:
		return true
	}
}

//Checks for any orders above current floor
func evaluateAboveOrders(currentOrders DistributeOrders.OrderUpdate, currentFloor int) bool {
	for floor := currentFloor + 1; floor < NFLOORS; floor++ {
		if currentOrders.State.CabRequests[floor] {			//Iterate through floors
			return true
		}
		for button := 0; button < 2; button++ {
			if currentOrders.DistributedOrders[floor][button] {
				return true
			}
		}
	}
	return false
}

//Checks any orders below current floor
func evaluateBelowOrders(currentOrders DistributeOrders.OrderUpdate, currentFloor int) bool {
	for floor := 0; floor < currentFloor; floor++ {
		if currentOrders.State.CabRequests[floor] {		//Iterate through floors
			return true
		}
		for button := 0; button < 2; button++ {
			if currentOrders.DistributedOrders[floor][button] {
				return true
			}
		}

	}
	return false
}
//Evaluate orders below and above, choose optimal direction
func chooseDirection(currentOrders DistributeOrders.OrderUpdate, floor int) elevio.MotorDirection {

	switch currentOrders.State.Direction {
	case "stop":
		fallthrough
	case "up":
		if evaluateAboveOrders(currentOrders, floor) {
			return elevio.MD_Up
		} else if evaluateBelowOrders(currentOrders, floor) {
			return elevio.MD_Down
		} else {
			return elevio.MD_Stop
		}

	case "down":
		if evaluateBelowOrders(currentOrders, floor) {
			return elevio.MD_Down
		} else if evaluateAboveOrders(currentOrders, floor) {
			return elevio.MD_Up
		} else {
			return elevio.MD_Stop
		}
	default:
		return elevio.MD_Stop
	}
}
