#!/usr/bin/env bash

# This script orchestrates the local Raft cluster environment.
# It launches three server nodes in the background, manages their standard output,
# and starts the interactive client in the foreground.

# Exit immediately if any command fails
set -e

# Configuration variables
PEERS_FILE="config/peers.txt"
NODE_1="127.0.0.1:2001"
NODE_2="127.0.0.1:2002"
NODE_3="127.0.0.1:2003"
NODE_4="127.0.0.1:2004"
NODE_5="127.0.0.1:2005"

# cleanup acts as the teardown hook for the cluster.
# It ensures no orphaned Go processes are left running on the specified UDP ports.
cleanup() {
    echo -e "\n[Cluster Manager] Shutting down all Raft nodes..."
    # pkill -P $$ terminates all child processes spawned by this specific script instance
    pkill -P $$ || true
    echo "[Cluster Manager] Teardown complete."
}

# Bind the cleanup function to the EXIT signal.
# This guarantees cleanup runs whether the script ends naturally or is interrupted (e.g., Ctrl+C).
trap cleanup EXIT

echo "[Cluster Manager] Booting Raft Node 1 ($NODE_1)..."
# Launch in background (&) and redirect all output to a dedicated log file
go run ./src/raftserver "$NODE_1" "$PEERS_FILE" > node1_stdout.log 2>&1 &

echo "[Cluster Manager] Booting Raft Node 2 ($NODE_2)..."
go run ./src/raftserver "$NODE_2" "$PEERS_FILE" > node2_stdout.log 2>&1 &

echo "[Cluster Manager] Booting Raft Node 3 ($NODE_3)..."
go run ./src/raftserver "$NODE_3" "$PEERS_FILE" > node3_stdout.log 2>&1 &

echo "[Cluster Manager] Booting Raft Node 4 ($NODE_3)..."
go run ./src/raftserver "$NODE_4" "$PEERS_FILE" > node4_stdout.log 2>&1 &

echo "[Cluster Manager] Booting Raft Node 5 ($NODE_3)..."
go run ./src/raftserver "$NODE_5" "$PEERS_FILE" > node5_stdout.log 2>&1 &

echo "[Cluster Manager] Waiting 3 seconds for the cluster to hold a Leader Election..."
sleep 3

echo "[Cluster Manager] Cluster is online."
echo "[Cluster Manager] Server outputs are being redirected to node*_stdout.log."
echo "--------------------------------------------------------------------------------"

# Launch the interactive client in the foreground, connecting to Node 1.
# Since it runs in the foreground, blocking the script, typing 'exit' here
# will end the script and trigger the cleanup trap.
go run ./src/raftclient "$NODE_1"
