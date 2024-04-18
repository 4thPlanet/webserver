 document.addEventListener("DOMContentLoaded", () => {
  console.log("DOM Content is loaded.")

  if (location.pathname == "/") {
    // No need for nav bar at top of page on home page
    document.getElementsByTagName("nav")[0].remove()
  }

  if (location.pathname == "/countdown") {
    const eventSource = new EventSource('');
    // on message append the message to #countdown
    const ul = document.getElementById("countdown")
    eventSource.onmessage = function(event) {
        console.log("Received message!", event)
        const li = document.createElement("li")
        li.innerText = event.data
        ul.append(li)
        if (event.data == "0") {
          eventSource.close()
        }
    };
  } else if (location.pathname == "/echo") {
    const socket = new WebSocket("ws://localhost:8080/echo")
    const memo = document.getElementById("memo")
    const submit = document.getElementById("submit")
    const echo = document.getElementById("echo")
    socket.addEventListener("open", () => {
      openSocket = true
    })
    socket.addEventListener("message", (event) => {
      echo.innerText = event.data
    })
    socket.addEventListener("close", (event) => {
      memo.disabled=disabled
    })
    submit.addEventListener("click", () => {
      socket.send(memo.value)
    })
  }


})
