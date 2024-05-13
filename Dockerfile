FROM golang:latest

WORKDIR /code
RUN apt-get update
RUN apt-get -y install python3
RUN apt-get -y install python3-setuptools
RUN apt-get -y install python3-pip
RUN apt-get install -y ffmpeg
RUN apt-get install -y yt-dlp
COPY . .
RUN go mod download
RUN mkdir temp_vids
CMD ["go", "run", "main.go"]