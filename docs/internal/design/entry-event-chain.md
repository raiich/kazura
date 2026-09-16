# How the event returned by Entry is processed

Entry runs inside a transition and cannot start another one, so it returns the
next event through state.Trigger and the machine processes it after Entry
returns. The loop ends when an Entry returns nil.

```mermaid
sequenceDiagram
    participant a as Caller
    participant m as StateMachine
    participant sf as State (from)
    participant st as State (to)
    participant t as Tracer

    a ->> m: Trigger(event)
    activate m

    loop the event, then each Trigger(event) an Entry returns
        m ->> sf: exit action(event)
        activate sf
        sf -->> m: nil (not guarded)
        deactivate sf

        Note over m: end the visit (cancel its timers, invalidate its handles) and start one of the destination, now the current state

        m ->> t: Trace(Transition{From, To, Event})

        m ->> st: Entry(EntryMachine, event)
        activate st
        st -->> m: Trigger(next event), Stop() or nil
        deactivate st
    end

    m -->> a: done
    deactivate m
```

Launch enters the initial state through the same loop, starting at the Trace of
a Transition with a zero From.
