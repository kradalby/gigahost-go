#!/usr/bin/env python3
"""Repeat Terraform acceptance tests; keep raw logs private and compare outcomes."""

import argparse
import csv
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
from datetime import datetime, timezone


def utc_now():
    return datetime.now(timezone.utc).isoformat()


def write_json(path, data):
    path.write_text(json.dumps(data, indent=2) + "\n")


def events(path):
    if not path.exists():
        return
    with path.open() as stream:
        for line in stream:
            try:
                yield json.loads(line)
            except json.JSONDecodeError:
                continue  # A running process may still be writing the last event.


def matrix_timings(path):
    stages = {}
    order_id = None
    initial_server_id = None
    provisioning = True
    for event in events(path):
        stages.setdefault(event["stage"], event)
        if event["stage"] in ("ssh_started", "terraform_error", "cleanup_started", "cleanup_retained"):
            provisioning = False
        if event["stage"] != "api_response":
            continue
        response = event["data"]
        if provisioning:
            stages["last_provision_observation"] = event
        if not 200 <= response["status_code"] < 300:
            continue
        data = response["body"].get("data")
        if response["path"] == "/deploy/servers" and isinstance(data, dict):
            orders = data.get("order_ids", [])
            if orders and order_id is None:
                order_id = str(orders[0])
        if response["path"] == "/deploy/status" and isinstance(data, dict):
            for server in data.get("servers", []):
                if str(server.get("order_id")) != order_id:
                    continue
                if server.get("srv_id") not in (None, "", 0, "0"):
                    stages.setdefault("allocated", event)
                    if initial_server_id is None:
                        initial_server_id = str(server["srv_id"])
                if server.get("status") in ("ready", "rescue", "iso"):
                    stages.setdefault("api_ready", event)
        if response["path"].startswith("/servers/") and isinstance(data, list):
            for server in data:
                if server.get("srv_id") not in (None, "", 0, "0"):
                    stages.setdefault("allocated", event)
                    if initial_server_id is None:
                        initial_server_id = str(server["srv_id"])
                if server.get("srv_status_install") in (False, 0, "0") and server.get("srv_status") in (True, 1, "1"):
                    stages.setdefault("api_ready", event)

    def duration(start, end):
        if start not in stages or end not in stages:
            return None
        return round(stages[end]["elapsed_seconds"] - stages[start]["elapsed_seconds"], 3)

    return {
        "order_id": order_id,
        "initial_server_id": initial_server_id,
        "submitted_at": stages.get("deploy_submitted", {}).get("at"),
        "allocated_at": stages.get("allocated", {}).get("at"),
        "api_ready_at": stages.get("api_ready", {}).get("at"),
        "ssh_finished_at": stages.get("ssh_finished", {}).get("at"),
        "allocation_seconds": duration("deploy_submitted", "allocated"),
        "submission_to_api_ready_seconds": duration("deploy_submitted", "api_ready"),
        "submission_to_last_provision_observation_seconds": duration("deploy_submitted", "last_provision_observation"),
        "ssh_seconds": duration("ssh_started", "ssh_finished"),
        "cleanup_seconds": duration("cleanup_started", "cleanup_finished"),
        "cleanup_status_code": stages.get("cleanup_finished", {}).get("data", {}).get("status_code"),
        "total_seconds": duration("case_started", "case_finished"),
        "ssh_error": stages.get("ssh_finished", {}).get("data", {}).get("error"),
    }


def summarize(root):
    tests = []
    matrix = []
    server_tests = []
    for round_dir in sorted(root.glob("round-*")):
        for log in sorted((round_dir / "private").glob("*-events.jsonl")):
            for event in events(log):
                if event.get("Test") and event.get("Action") in ("pass", "fail", "skip"):
                    tests.append({
                        "round": round_dir.name,
                        "suite": log.name.removesuffix("-events.jsonl"),
                        "test": event["Test"],
                        "outcome": event["Action"],
                        "elapsed_seconds": event.get("Elapsed"),
                    })
        for result in sorted((round_dir / "matrix").glob("*/*/share/result.json")):
            row = json.loads(result.read_text())
            case = result.parents[1]
            timings = matrix_timings(result.with_name("events.jsonl"))
            ready_seconds = timings["submission_to_api_ready_seconds"]
            observed_seconds = ready_seconds if ready_seconds is not None else timings["submission_to_last_provision_observation_seconds"]
            matrix.append({
                "round": round_dir.name,
                "os": case.parent.name,
                "auth": case.name,
                "outcome": "pass" if row["passed"] else "fail",
                "server_id": row["server_id"],
                "retained": row["retained"],
                **timings,
                "exceeded_default_create_timeout": observed_seconds > 900 if observed_seconds is not None else None,
            })
        for result in sorted((round_dir / "suite").glob("*/share/result.json")):
            row = json.loads(result.read_text())
            server_tests.append({
                "round": round_dir.name,
                "test": row["case"],
                "outcome": "pass" if row["passed"] else "fail",
                "server_id": row["server_id"],
                **matrix_timings(result.with_name("events.jsonl")),
            })
    stability = {}
    for row in tests:
        counts = stability.setdefault(row["test"], {"pass": 0, "fail": 0, "skip": 0})
        counts[row["outcome"]] += 1
    write_json(root / "tests.json", tests)
    write_json(root / "stability.json", stability)
    write_json(root / "matrix.json", matrix)
    write_json(root / "server-tests.json", server_tests)
    if matrix:
        with (root / "matrix.tsv").open("w") as stream:
            writer = csv.DictWriter(stream, fieldnames=list(matrix[0]), delimiter="\t")
            writer.writeheader()
            writer.writerows(matrix)
    return tests


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rounds", type=int, default=3)
    parser.add_argument("--parallel", type=int, default=4)
    parser.add_argument("--directory", type=Path)
    parser.add_argument("--resume", type=Path, help="Continue an interrupted experiment after its active test command finishes")
    parser.add_argument("--timeout", default="12h", help="Outer Go test timeout; cell create and SSH deadlines remain independent")
    parser.add_argument("--summarize", type=Path, help="Summarize an existing or running experiment")
    args = parser.parse_args()
    os.umask(0o077)
    if args.summarize:
        summarize(args.summarize.resolve())
        return 0
    if args.rounds < 2 or args.parallel < 1:
        parser.error("use at least two rounds and positive parallelism")
    if not os.environ.get("GIGAHOST_TOKEN"):
        parser.error("GIGAHOST_TOKEN is required; run through direnv")
    if os.environ.get("CI"):
        parser.error("repeat matrix is local and explicitly opt-in")

    root = (args.resume or args.directory or Path(".direnv") / datetime.now(timezone.utc).strftime("acceptance-repeat-%Y%m%dT%H%M%SZ")).resolve()
    if not args.resume:
        root.mkdir(parents=True, exist_ok=False)
    lock = (root / ".runner.lock").open("w")
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        parser.error("experiment already has an active scheduler")
    if args.resume:
        metadata = json.loads((root / "run.json").read_text())
        args.rounds = metadata["rounds"]
        args.parallel = metadata["parallel"]
        for entry in metadata["commands"]:
            if "exit_code" in entry:
                continue
            log = root / f"round-{entry['round']:02d}" / "private" / f"{entry['suite']}-events.jsonl"
            terminal = [e for e in events(log) if not e.get("Test") and e.get("Action") in ("pass", "fail")]
            if not terminal:
                parser.error("active command has no terminal result; allow it to finish cleanup before resuming")
            entry.update(exit_code=0 if terminal[-1]["Action"] == "pass" else 1,
                         finished_at=terminal[-1].get("Time"), exit_code_from_test_events=True)
        metadata.pop("finished_at", None)
        metadata.setdefault("resumes", []).append({"at": utc_now(), "runner_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
    else:
        metadata = {
            "started_at": utc_now(), "rounds": args.rounds, "parallel": args.parallel,
            "commit": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
            "tracked_patch_sha256": hashlib.sha256(subprocess.check_output(["git", "diff", "HEAD"])).hexdigest(),
            "runner_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            "inputs": {name: os.environ[name] for name in (
                "GIGAHOST_TEST_DEPLOY_TYPE", "GIGAHOST_TEST_DEPLOY_SIZE",
                "GIGAHOST_TEST_ZONE_APEX", "GIGAHOST_TEST_ZONE_NAME",
                "GIGAHOST_TEST_MATRIX_KEY_FILE",
            ) if name in os.environ},
            "create_timeout": os.environ.get("GIGAHOST_TEST_MATRIX_CREATE_TIMEOUT", "45m"),
            "commands": [],
        }
    write_json(root / "run.json", metadata)
    print(f"Acceptance evidence: {root}", flush=True)
    failed = any(entry.get("exit_code", 0) != 0 for entry in metadata["commands"])
    env = os.environ.copy()
    if args.resume:
        env.update(metadata["inputs"])
        env["GIGAHOST_TEST_MATRIX_CREATE_TIMEOUT"] = metadata["create_timeout"]
        first_dir = root / "round-01" / "matrix"
        key = first_dir / "private" / "id_ed25519"
        if key.exists() and any(not json.loads(p.read_text())["passed"] for p in first_dir.glob("*/*/share/result.json")):
            env["GIGAHOST_TEST_MATRIX_KEY_FILE"] = str(key)
    env["GIGAHOST_TEST_OS_MATRIX"] = "1"
    env["GIGAHOST_TEST_SERVER_FIXTURE"] = "1"
    try:
        for round_number in range(1, args.rounds + 1):
            round_dir = root / f"round-{round_number:02d}"
            (round_dir / "private").mkdir(parents=True, exist_ok=True)
            env["GIGAHOST_TEST_MATRIX_DIR"] = str(round_dir / "matrix")
            env["GIGAHOST_TEST_KEEP_FAILED"] = "1" if round_number == 1 else "0"
            for suite, selection in (
                ("matrix", ["-run", "^TestAccServer_perOS$"]),
                ("suite", ["-run", "^TestAcc", "-skip", "^TestAccServer_perOS$"]),
            ):
                if any(entry["round"] == round_number and entry["suite"] == suite for entry in metadata["commands"]):
                    continue
                command = ["nix", "run", ".#test-acc", "--", "-json", "-count=1", f"-timeout={args.timeout}", f"-parallel={args.parallel}", *selection]
                entry = {"round": round_number, "suite": suite, "started_at": utc_now(), "command": command}
                metadata["commands"].append(entry)
                write_json(root / "run.json", metadata)
                print(f"Round {round_number}/{args.rounds}: {suite}", flush=True)
                with (round_dir / "private" / f"{suite}-events.jsonl").open("w") as output, (round_dir / "private" / f"{suite}-stderr.txt").open("w") as errors:
                    entry["exit_code"] = subprocess.call(command, env=env, stdout=output, stderr=errors)
                entry["finished_at"] = utc_now()
                failed |= entry["exit_code"] != 0
                summarize(root)
                write_json(root / "run.json", metadata)
                print(f"Round {round_number}: {suite} exit {entry['exit_code']}", flush=True)
                # Failed first-round deployments keep their key for debugging.
                results = list((round_dir / "matrix").glob("*/*/share/result.json"))
                key = round_dir / "matrix" / "private" / "id_ed25519"
                if suite == "matrix" and key.exists() and any(not json.loads(p.read_text())["passed"] for p in results):
                    env["GIGAHOST_TEST_MATRIX_KEY_FILE"] = str(key)
    finally:
        metadata["finished_at"] = utc_now()
        write_json(root / "run.json", metadata)
        summarize(root)
        lock.close()
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
