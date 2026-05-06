#!/bin/bash
# researcher-entrypoint.sh — runs first inside a social-researcher
# container. When TS_AUTHKEY is set in the env, brings tailscale up in
# userspace mode (no NET_ADMIN, no /dev/net/tun) and exec's into
# whatever command docker run passed in (bash by default, claude in
# --claude mode).
#
# When TS_AUTHKEY isn't set, the script falls through to exec
# immediately — zero overhead for operators not using tailscale.
#
# Userspace tailscaled needs no kernel privileges. The socket lives at
# /tmp/tailscaled.sock (the default /var/run/tailscale isn't writable
# by the non-root agent user); we export TS_SOCKET so subsequent
# `tailscale` CLI invocations Just Work without --socket.

set -e

if [ -n "${TS_AUTHKEY:-}" ]; then
    # Background tailscaled, capturing logs to /tmp for debugging.
    tailscaled \
        --tun=userspace-networking \
        --socket=/tmp/tailscaled.sock \
        --state=/tmp/tailscaled.state \
        > /tmp/tailscaled.log 2>&1 &

    # Wait up to ~5s for the socket to appear; tailscaled's startup
    # is fast in userspace mode but isn't instantaneous.
    for _ in $(seq 1 10); do
        [ -S /tmp/tailscaled.sock ] && break
        sleep 0.5
    done

    # Hostname: research-<short-container-id> by default, so a busy
    # operator can tell their tailnet entries apart. Ephemerality is
    # carried by the auth key itself (mark "Ephemeral" in the admin
    # UI when generating it) — older tailscale CLIs don't accept a
    # --ephemeral flag, so we don't pass one. The coordinator
    # auto-prunes the node ~5 min after disconnect.
    if ! tailscale --socket=/tmp/tailscaled.sock up \
        --authkey="$TS_AUTHKEY" \
        --hostname="research-$(hostname)" \
        --reset; then
        echo "researcher-entrypoint: tailscale up failed — continuing without tailnet" >&2
    fi

    # So `tailscale status` etc. inside the bash shell don't need
    # --socket=/tmp/tailscaled.sock typed by the operator.
    export TS_SOCKET=/tmp/tailscaled.sock
fi

exec "$@"
