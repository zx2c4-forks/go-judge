# go-judge

Under designing & development

[![GoDoc](https://godoc.org/github.com/criyle/go-judge?status.svg)](https://godoc.org/github.com/criyle/go-judge)

The goal to to reimplement [syzoj/judge-v3](https://github.com/syzoj/judge-v3) in GO language using [go-sandbox](https://github.com/criyle/go-sandbox).

## Planned Design

### Executor Service Draft

A rest service to run program in restricted environment and it is basically a wrapper for `pkg/envexec` to run single / multiple programs.

- /run POST execute program in the restricted environment
- /file POST prepare a file in the executor service (in memory), returns fileId (can be referenced in /run parameter)
- /file/:fileId GET downloads file from executor service (in memory), returns file content
- /file/:fileId DELETE delete file specified by fileId

Planed API interface:

```typescript
interface LocalFile {
    src: string; // absolute path for the file
}

interface MemoryFile {
    content: string | Buffer; // file contents
}

interface PreparedFile {
    fileId: string; // fileId defines file uploaded by /file
}

interface Pipe {
    max: number; // maximum bytes to collect from pipe
}

interface Cmd {
    args: string[]; // command line argument
    env?: string[]; // environment

    // specifies file input / pipe collector for program file descriptors
    files?: (LocalFile | MemoryFile | PreparedFile | Pipe)[];

    // limitations
    cpuLimit?: number;     // s
    readCpuLimit?: number; // s
    memoryLimit?: number;  // byte
    procLimit?: number;

    // copy the correspond file to the container dst path
    copyIn?: {[dst:string]:LocalFile | MemoryFile | PreparedFile};

    // copy out specifies files need to be copied out from the container after execution
    copyOut?: string[];
    // similar to copyOut but stores file in executor service and returns fileId, later download through /file/:fileId
    copyOutCached?: string[];
}

enum Status {
    Accepted,            // normal
    MemoryLimitExceeded, // mle
    TimeLimitExceeded,   // tle
    OutputLimitExceeded, // ole
    FileError,           // fe
    RuntimeError,        // re
    DangerousSyscall,    // dgs
    InternalError,       // system error
}

interface Result {
    status: Status;
    error?: string; // potential system error message
    time: number;   // ns
    memory: number; // byte
    // copyFile name -> content
    files?: {[name:string]:string};
    // copyFileCached name -> fileId
    fileIds?: {[name:string]:string};
}
```

### Workflow

``` text
    ^
    | Client (talk to the website)
    v
+------+    +----+
|      |<-->|Data|
|Judger|    +----+--+
|      |<-->|Problem|
+------+    +-------+
    ^
    | TaskQueue
    v
+------+   +--------+
|Runner|<->|Language|
+------+   +--------+
    ^
    | EnvExec
    v
+--------------------+
|ContainerEnvironment|
+--------------------+
```

### Container Root Filesystem

- [x] necessary lib / exec / compiler / header readonly bind mounted from current file system: /lib /lib64 /bin /usr
- [x] work directory tmpfs mount: /w (work dir), /tmp (compiler temp files)
- [ ] additional compiler scripts / exec readonly bind mounted: /c
- [ ] additional header readonly bind mounted: /i

### Interfaces

- client: receive judge tasks (websocket / socket.io / RabbitMQ / REST API)
- data: interface to download, cache, lock and access test data files from website (by dataId)
- taskqueue: message queue to send run task and receive run task result (In memory / (RabbitMQ, Redis))
- file: general file interface (disk / memory)
- language: programming language compile & execute configurations
- problem: parse problem definition from configuration files

### Judge Logic

- judger: execute judge logics (compile / standard / interactive / answer submit) and distribute as run task to queue, the collect and calculate results
- runner: receive run task and execute in sandbox environment

### Models

- JudgeTask: judge task pushed from website (type, source, data)
- JudgeResult: judge task result send back to website
- JudgeSetting: problem setting (from yaml) and JudgeCase
- RunTask: run task parameters send to run_queue
- RunResult: run task result sent back from queue

### Utilities

- pkg/envexec: run single / group of programs in parallel within restricted environment and resource constraints

## Planned API

### Progress

Client is able to report progress to the web front-end. Task should maintain its states

Planned events are:

- Parsed: problem data have been downloaded and problem configuration have been parsed (pass problem config to task)
- Compiled: user code have been compiled (success / fail)
- Progressed: single test case finished (success / fail - detail message)
- Finished: all test cases finished / compile failed

## TODO

- [x] socket.io client with namespace
- [x] judge_v3 protocol
- [ ] executor server
- [ ] syzoj problem YAML config parser
- [ ] syzoj data downloader
- [ ] syzoj compile configuration
- [ ] file io
- [ ] special judger
- [ ] interact problem
- [ ] answer submit
- [ ] demo site
