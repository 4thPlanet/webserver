 document.addEventListener("DOMContentLoaded", () => {
  console.log("DOM Content is loaded.")

  const eventSource = new EventSource('/sse');
  eventSource.onmessage = function(event) {
      console.log("Received message!", event)
      if (event.data == "0") {
        eventSource.close()
      }
  };

  const socket = new WebSocket("ws://localhost:8080/websocket",["foobar"]);
  let openSocket = false
  socket.addEventListener("open", () => {
    openSocket = true
    let reset = setInterval(() => {
      if (openSocket) {
        socket.send(100)
      } else {
        clearInterval(reset)
      }
    }, 10000)

  })
  socket.addEventListener("message", (event) => {
    console.log("Message from server ", event.data);
  });
  socket.addEventListener("close", event => {
    console.log("Closed websocket connection:", event)
    openSocket = false
  })

})
