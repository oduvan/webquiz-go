"""Record the JSON messages of the file transfer of WebQuiz 1.19 (Python) and
write python_transfer.json next to this file.

Two Python TransferManager objects ("room-a" sends, "room-b" receives) talk
through a small fake tunnel server, like tests/test_transfer.py. The fake
tunnel records every request between them. The script also records what the
managers return to the admin pages and the messages they send to the admin
WebSocket.

TestTransferJSONMatchesPython (match_python_test.go) runs the same steps with
the Go manager and checks that Go sends and answers the same JSON: the same
keys, the same value types, null where Python wrote null, and the same
values. Ids, tokens and times are random: both sides replace each one with a
placeholder in the order they first appear ("<id1>", "<token1>", "<time>").

Run it from the Python project:

    cd /home/user/webquiz && source venv/bin/activate && \\
        PYTHONPATH=/home/user/webquiz python \\
        <worktree>/internal/transfer/testdata/gen_python_transfer.py
"""

import asyncio
import json
import os
import shutil
import sys
import tempfile

import yaml
from aiohttp import web
from aiohttp.test_utils import TestServer

import webquiz
from webquiz.transfer import TransferError, TransferManager

KINDS = ("quizzes", "logs", "csv", "images", "attachments")

GEO_QUIZ = """# Geography quiz
title: Geography
questions:
- question: Where is Kyiv?
  image: "/imgs/map.png"  # the main map
  options: ["/imgs/a.png", "Lviv"]
  correct_answer: 0
  explanation: |
    See ![map](</imgs/my map.png>) for the rivers.
- question: Read the notes
  file: "нотатки 1.pdf"
  options: ["yes", "no"]
  correct_answer: 0
"""

# The files of each server before the transfer: "<kind>/<name>": text
SETUP = {
    "room-a": {
        "quizzes/geo.yaml": GEO_QUIZ,
        "quizzes/same.yaml": "title: Same\n",
        "quizzes/diff.yaml": "title: From A\n",
        "quizzes/active.yaml": "title: Active on A\n",
        "quizzes/math.yaml": "title: Math\n",
        "logs/0001.log": "log line\n",
        "csv/results.csv": "a,b\r\n1,2\r\n",
        "images/map.png": "image map of A",
        "images/a.png": "image a",
        "images/my map.png": "image my map",
        "attachments/нотатки 1.pdf": "notes",
    },
    "room-b": {
        "quizzes/same.yaml": "title: Same\n",
        "quizzes/diff.yaml": "title: From B\n",
        "quizzes/active.yaml": "title: Active on B\n",
        "logs/room-a_0001.log": "older log\n",
        "images/map.png": "image map of B",
    },
}

# Files the receiver's running server uses (current log, CSV files, active quiz)
IN_USE = {"room-b": ["quizzes/active.yaml"]}

OFFERED = [
    {"type": "quizzes", "name": "geo.yaml"},
    {"type": "quizzes", "name": "same.yaml"},
    {"type": "quizzes", "name": "diff.yaml"},
    {"type": "quizzes", "name": "active.yaml"},
    {"type": "logs", "name": "0001.log"},
    {"type": "csv", "name": "results.csv"},
]

# What the receiver's admin chooses; the other files get their default action
CHOICES = [
    {"type": "quizzes", "name": "diff.yaml", "action": "replace"},
    {"type": "quizzes", "name": "active.yaml", "action": "rename", "new_name": "Активний.yaml"},
    {"type": "logs", "name": "0001.log", "action": "skip"},
]

# Offers that another server could send: the receiver must store them as Python did
CRAFTED_OFFER = {
    "from": "room-a",
    "token": "t" * 43,
    "files": [
        {"type": "logs", "name": "float.log", "size": 12.0, "sha256": "0" * 64},
        {"type": "logs", "name": "bool.log", "size": True, "sha256": "not a checksum"},
        {"type": "logs", "name": "negative.log", "size": -5},
        {"type": "csv", "name": "big.csv", "size": 123456789012345678901234567890},
        {"type": "attachments", "name": "README", "size": 7, "sha256": None},
        {"type": "attachments", "name": "README", "size": 7},
    ],
}


class Machine:
    """One WebQuiz server with its own folders"""

    def __init__(self, root, name, tunnel):
        self.name = name
        self.tunnel = tunnel
        self.online = True  # the tunnel server reaches it
        self.connected = True  # its own tunnel is connected (get_endpoint)
        self.dirs = {kind: os.path.join(root, name, kind) for kind in KINDS}
        for path in self.dirs.values():
            os.makedirs(path)
        self.messages = []
        self.files_in_use = []
        self.manager = TransferManager(
            get_directories=lambda: self.dirs,
            get_endpoint=lambda: (self.tunnel.base_url, self.name) if self.connected else None,
            notify=self._notify,
            get_files_in_use=lambda: self.files_in_use,
        )

    async def _notify(self, message):
        self.messages.append(json.loads(json.dumps(message)))

    def write(self, relative, text):
        kind, name = relative.split("/", 1)
        with open(os.path.join(self.dirs[kind], name), "w", encoding="utf-8", newline="") as f:
            f.write(text)

    def files(self):
        """Every file of the machine: "<kind>/<name>": text (quizzes as their YAML data)"""
        result = {}
        for kind in KINDS:
            for name in sorted(os.listdir(self.dirs[kind])):
                with open(os.path.join(self.dirs[kind], name), encoding="utf-8", newline="") as f:
                    text = f.read()
                result[f"{kind}/{name}"] = {"yaml": yaml.safe_load(text)} if kind == "quizzes" else text
        return result


class FakeTunnelServer:
    """Routes "/start/{name}/api/transfer/..." to the manager of that machine"""

    def __init__(self):
        self.machines = {}
        self.base_url = None
        self.log = []

    def _manager(self, request):
        machine = self.machines.get(request.match_info["name"])
        if machine is None or not machine.online:
            raise web.HTTPBadGateway()
        return machine.manager

    async def offer(self, request):
        return await self._manager(request).handle_offer(request)

    async def download(self, request):
        return await self._manager(request).handle_download(request)

    async def result(self, request):
        return await self._manager(request).handle_result(request)

    @web.middleware
    async def record(self, request, handler):
        body = await request.read()
        try:
            response = await handler(request)
        except web.HTTPException as error:
            response = error
        entry = {
            "method": request.method,
            "path": request.raw_path,
            "content_type": request.headers.get("Content-Type"),
            "body": decode(body),
            "status": response.status,
            "response_content_type": response.headers.get("Content-Type"),
        }
        if isinstance(response, web.Response) and response.body is not None:
            entry["response"] = decode(response.body)
        self.log.append(entry)
        return response

    def app(self):
        app = web.Application(middlewares=[self.record])
        app.router.add_post("/start/{name}/api/transfer/offer", self.offer)
        app.router.add_get("/start/{name}/api/transfer/download/{token}/{type}/{filename}", self.download)
        app.router.add_post("/start/{name}/api/transfer/result", self.result)
        return app


def decode(body):
    """A recorded body: parsed JSON, else text, else None for no body"""
    if body is None:
        return None
    if isinstance(body, (bytes, bytearray)):
        if not body:
            return None
        text = bytes(body).decode("utf-8", errors="replace")
    else:
        text = str(body)
    try:
        return {"json": json.loads(text)}
    except ValueError:
        return {"text": text}


def transfer_error(error):
    return {"status": error.status, "error": error.message}


async def main(output):
    root = tempfile.mkdtemp()
    fake = FakeTunnelServer()
    server = TestServer(fake.app())
    await server.start_server()
    fake.base_url = str(server.make_url("/start"))
    captures = {}
    try:
        machines = {name: Machine(root, name, fake) for name in ("room-a", "room-b", "room-c")}
        fake.machines.update(machines)
        a, b, c = machines["room-a"], machines["room-b"], machines["room-c"]
        for name, files in SETUP.items():
            for relative, text in files.items():
                machines[name].write(relative, text)
        for name, files in IN_USE.items():
            machines[name].files_in_use = [None] + [
                os.path.join(machines[name].dirs[f.split("/", 1)[0]], f.split("/", 1)[1]) for f in files
            ]

        # 1. A offers files to B
        captures["send_return"] = json.loads(json.dumps(await a.manager.send_offer("room-b", OFFERED)))
        captures["offer_http"] = fake.log[-1]
        captures["receiver_request_message"] = b.messages[-1]
        captures["receiver_state"] = b.manager.get_state()
        captures["sender_state_waiting"] = a.manager.get_state()

        # 2. B accepts with some choices
        request_id = b.manager.list_incoming()[0]["id"]
        mark = len(fake.log)
        captures["accept_return"] = await b.manager.accept(request_id, CHOICES)
        captures["accept_http"] = fake.log[mark:]
        captures["receiver_removed_message"] = b.messages[-1]
        captures["sender_result_message"] = a.messages[-1]
        captures["sender_state_accepted"] = a.manager.get_state()
        captures["receiver_files"] = b.files()

        # 3. A offers another file, B rejects it
        await a.manager.send_offer("room-b", [{"type": "quizzes", "name": "math.yaml"}])
        request_id = b.manager.list_incoming()[0]["id"]
        mark = len(fake.log)
        await b.manager.reject(request_id)
        captures["reject_http"] = fake.log[mark:]
        captures["reject_removed_message"] = b.messages[-1]
        captures["reject_result_message"] = a.messages[-1]

        # 4. An offer with odd sizes and checksums, as another program could send it
        mark = len(fake.log)
        async with __import__("httpx").AsyncClient() as client:
            await client.post(f"{fake.base_url}/room-b/api/transfer/offer", json=CRAFTED_OFFER)
        captures["crafted_offer_http"] = fake.log[mark:]
        captures["crafted_request_message"] = b.messages[-1]

        # 5. Errors of the public endpoints
        mark = len(fake.log)
        token = "x" * 43
        async with __import__("httpx").AsyncClient() as client:
            base = fake.base_url
            await client.post(f"{base}/room-b/api/transfer/offer", content=b"not json")
            await client.post(f"{base}/room-b/api/transfer/offer", json=["not", "a dict"])
            await client.post(f"{base}/room-b/api/transfer/offer", json={"from": "../x", "token": token, "files": []})
            await client.post(f"{base}/room-b/api/transfer/offer", json={"from": "room-a", "token": "short", "files": []})
            await client.post(f"{base}/room-b/api/transfer/offer", json={"from": "room-a", "token": token, "files": []})
            await client.post(
                f"{base}/room-b/api/transfer/offer",
                json={"from": "room-a", "token": token, "files": [{"type": "logs", "name": "../a.log"}]},
            )
            await client.get(f"{base}/room-a/api/transfer/download/{token}/quizzes/geo.yaml")
            await client.post(f"{base}/room-a/api/transfer/result", content=b"not json")
            await client.post(f"{base}/room-a/api/transfer/result", json={"token": token, "status": "hacked"})
            await client.post(f"{base}/room-a/api/transfer/result", json={"token": token, "status": "accepted"})
            b.connected = False
            await client.post(
                f"{base}/room-b/api/transfer/offer",
                json={"from": "room-a", "token": token, "files": [{"type": "logs", "name": "a.log"}]},
            )
            b.connected = True
            for _ in range(21):
                await client.post(
                    f"{base}/room-c/api/transfer/offer",
                    json={"from": "room-a", "token": token, "files": [{"type": "logs", "name": "a.log"}]},
                )
        captures["public_errors_http"] = [entry for entry in fake.log[mark:] if entry["status"] != 202]

        # 6. Errors that the sender's admin sees
        errors = {}
        for name, target in (("offline", "room-x"), ("refused", "room-b")):
            if name == "refused":
                b.connected = False
            try:
                await a.manager.send_offer(target, [{"type": "quizzes", "name": "math.yaml"}])
            except TransferError as error:
                errors[name] = transfer_error(error)
            b.connected = True
        a.connected = False
        for name, call in (
            ("send_without_tunnel", lambda: a.manager.send_offer("room-b", OFFERED)),
            ("accept_unknown", lambda: b.manager.accept("unknown")),
            ("reject_unknown", lambda: b.manager.reject("unknown")),
        ):
            try:
                await call()
            except TransferError as error:
                errors[name] = transfer_error(error)
        captures["admin_errors"] = errors
        captures["state_without_tunnel"] = a.manager.get_state()
    finally:
        await server.close()
        shutil.rmtree(root)

    result = {
        "webquiz_version": webquiz.__version__,
        "python_version": sys.version.split()[0],
        "setup": SETUP,
        "in_use": IN_USE,
        "offered": OFFERED,
        "choices": CHOICES,
        "crafted_offer": CRAFTED_OFFER,
        "captures": captures,
    }
    with open(output, "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
        f.write("\n")


if __name__ == "__main__":
    asyncio.run(main(os.path.join(os.path.dirname(os.path.abspath(__file__)), "python_transfer.json")))
