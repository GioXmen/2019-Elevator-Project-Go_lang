NTNU: Elevator Project Spring 2019
================

This respository contains code for the elevator project in TTK4145 in the spring of 2019.

Libraries used
--------------
This project makes use of already written code; the hall request assigner, which distributes the orders of multiple elevators on a network, a single elevator algorithm, a Network module for Go and an elevator interface elev_io.go, all of which which were provided by GitHub user @klasbo.

Project structure and modules:
------------------------------

Main.go:
The main function is the entry point of the system consisting of 5 main modules:
Main, FSM, ElevState, DistributeOrders and Network.
The communication between the modules is indicated where the channels are created, however, an
overview will be given here:
ElevState sends the orders and state of all the elevators to DistributedOrders, which then calculates the optimal order distribution for this elevator.
DistributeOrders then sends the calculated orders to FSM which stores them. When an event occurs, FSM sends the event type and what action was taken to ElevState.
Upon receiving this update, ElevState communicates this to the Network module, which passes it on to all the other elevators.
When receiving the update from an elevator's Network module, the receiving Network modules sends the updated states
to their ElevState modules which stores them.

FSM.go:
The FSM module acts on the current orders received from the DistributeOrders module and sends what it did
to the ElevState button. It receives an OrderUpdate type (defined in DistributeOrders) containing this elevator's
orders and its current state. It sends out messages of type EventMessage (defined in ElevState) to ElevState informing
it of changes made to the elevator states and/or orders.

DistributeOrders.go:
The DistributeOrders module take in state information of all elevators and uses hall_request_assigner to calculate
Which elevator should take which order. It access the hall_request_assigner by using terminal commands.
The information is sent as a JSON version of AllStates (defined in ElevState). Only the relevant information
for the FSM is sent out of this module. For this implementation only the local elevators orders and state is
relevant for the FSM.

Network.go (and all of the included sub-modules):
The Network module handles sending and receiving NetworkMessages and peer information over the network
to all it's peers. It both gets and sends it's information to the ElevState module.

ElevState.go:
The ElevState handles everything that has to do with changes in the local data over every elevator sate
hall requests and cab requests. It gets it's information from the Network, the FSM and from buttons pressed.
It sends information to the DistributeOrders and Network modules. It also sets hall request and cab request lights
and define most of the own defined struct's in this program.

elevator_states.txt:
This is where our state backup is stored. If an elevator is crashed or restarted, it will restore its states and orders from this file

hall_request_assigner executable:
Compiled executable of the hall request assigner code, used in distribute orders module.

elev_io.go:
Elevator driver, that is used to communicate with the simulator and hardware elevator

Simulator:
Used to test elevator. Run with ./SimElevatorServer
