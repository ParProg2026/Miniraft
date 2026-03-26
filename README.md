### Raft Implementation in Go

You can find the overall project structure in the next section.
The documentation is part of the comments inside the code itself.

#### Project Structure

- **`src/raftserver/`**: Contains the Raft server implementation.
  - `main.go`: Entry point for the server. Handles CLI arguments, seeds the random number generator, initializes the network and state, and runs the main event loop.
  - `state.go`: Defines the `RaftServer` structure and state transitions (Follower, Candidate, Leader).
  - `network.go`: Manages UDP connections, including sending, broadcasting, and listening for messages.
  - `rpc.go`: Implements the handlers for Raft RPCs (AppendEntries, RequestVote) and client commands.
  - `election.go`: Contains logic for starting elections and sending heartbeats.
  - `decoder.go`: Provides strict JSON decoding for incoming UDP packets to distinguish between different message types.
- **`src/raftclient/`**: Contains the Raft client implementation.
  - `raftclient.go`: A simple CLI client that sends alphanumeric commands to a Raft node.
- **`src/protocol/`**: Shared protocol definitions.
  - `miniraft.go`: Defines the JSON structures for Raft messages (AppendEntries, RequestVote) and log entries.
- **`src/start.sh`**: Orchestration script to launch a 3-node local cluster and an interactive client.
- **`config/peers.txt`**: Configuration file listing the addresses of all nodes in the cluster.
- **`src/servers.txt`**: Alternative server list (if applicable).
- **`src/go.mod`**: Go module definition.

#### How to Run

1. **Start the Cluster**:
   Use the provided script to boot three nodes and a client:
   ```bash
   ./src/start.sh
   ```
   The script will:
   - Start 3 nodes in the background (output redirected to `node*_stdout.log`).
   - Wait for a leader to be elected.
   - Launch the interactive client connected to the first node.

2. **Manual Execution**:
   - To start a server:
     ```bash
     go run ./src/raftserver <host:port> <peers_file>
     ```
   - To start a client:
     ```bash
     go run ./src/raftclient <server_host:port>
     ```

#### Interaction and Debugging

- **Client**: Type any alphanumeric string to send it as a command to the Raft cluster. Type `exit` to quit.
- **Server Debug Commands**: You can interact with individual server nodes via their `stdin` if running manually:
  - `print`: Displays the current internal state (Term, Role, Log, etc.).
  - `log`: Dumps the current log entries.
  - `suspend`: Simulates a node crash/failure.
  - `resume`: Resumes a suspended node.
