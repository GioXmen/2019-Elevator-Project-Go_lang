package Network

/* The Network module handles sending and receiving NetworkMessages and peer information over the network
to all it's peers. It both gets and sends it's information to the ElevState module.
*/

import (
	"../ElevState"
	"./network/bcast"
	"./network/peers"
	"fmt"
	"time"
)

var ID string

//Function that handles all sending and receiving over the network
func Network(PeerState chan<- ElevState.NetworkMessage, UpdatedPeers chan<- peers.PeerUpdate, MsgToNetwork <-chan ElevState.NetworkMessage, ID string) {

	// We make a channel for receiving id of peers on the network
	peerUpdateCh := make(chan peers.PeerUpdate)
	//Make a channel that contains the bool tha enables or disables the peerUpdateCh
	peerTxEnable := make(chan bool)

	//Put the channels into the peers modules function
	go peers.Transmitter(15432, ID, peerTxEnable)
	go peers.Receiver(15432, peerUpdateCh)

	// We make channels for sending and receiving our NetworkMessage struct
	Tx := make(chan ElevState.NetworkMessage)
	Rx := make(chan ElevState.NetworkMessage)

	//Enter the channels into the bcast functions
	go bcast.Transmitter(16789, Tx)
	go bcast.Receiver(16789, Rx)

	fmt.Println("Started Network Module")

	//Inits the variable that is used for saving the last NetworkMessage received from ElevState
	// and a timer that makes the Transmitter send the LastPackageFromLocal every 100 Millisecond   -- Our fault tolerance solution
	timeOut := time.NewTimer(100 * time.Millisecond)
	lastPackageFromLocal := ElevState.NetworkMessage{}

	for {
		select {

		case peersInfo := <-peerUpdateCh: //Prints the peer list update then sends the update to ElevState
			fmt.Printf("Peer update:\n")
			fmt.Printf("  Peers:    %q\n", peersInfo.Peers)
			fmt.Printf("  New:      %q\n", peersInfo.New)
			fmt.Printf("  Lost:     %q\n", peersInfo.Lost)
			//Sends the updated peers information to the ElevState
			UpdatedPeers <- peersInfo

		case received := <-Rx: //send the received NetworkMessage to ElevState
			PeerState <- received

		case packageFromLocal := <-MsgToNetwork: //The case that handles transmitting to Network
			// Update lastPackageFromLocal to the new message
			lastPackageFromLocal = packageFromLocal

		case <-timeOut.C: //Handles the message when the timer runs out

			if lastPackageFromLocal.MessageType == "MotorProblems" { //if it is has motor problems it disconnects from the network
				peerTxEnable <- false
				fmt.Println("Disconnect from network on resend")

			} else if lastPackageFromLocal.MessageType == "MotorWorksAgain" { //if the motor starts working again it reconnects
				peerTxEnable <- true

			} else { //for all other cases it check that the message has an ID and then sends it
				if lastPackageFromLocal.ID != "" {
					Tx <- lastPackageFromLocal
				}

				//finally it resests the timer to 100 Millisecond
				timeOut.Reset(100 * time.Millisecond)
			}
		}
	}
}
