FROM golang:latest

WORKDIR /code
RUN apt-get update
RUN apt-get -y install python3
RUN apt-get -y install python3-setuptools
RUN apt-get -y install python3-pip
RUN apt-get install -y ffmpeg
RUN apt-get install -y yt-dlp
RUN git clone https://github.com/erik-petrov/dj_sanya_go
WORKDIR /code/dj_sanya_go
COPY .env .
RUN git clone https://github.com/erik-petrov/dca
RUN go mod tidy
CMD ["go", "run", "main.go"]