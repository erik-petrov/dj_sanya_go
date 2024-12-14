FROM golang:latest

WORKDIR /code
RUN apt-get update
RUN apt-get -y install python3
RUN apt-get -y install python3-setuptools
RUN apt-get -y install python3-pip
RUN apt-get install -y ffmpeg
RUN curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o ~/bin/yt-dlp --create-dirs && chmod a+rx ~/bin/yt-dlp
COPY . .
ENV PATH="$PATH:/$HOME/bin/"
RUN go mod download
RUN mkdir temp_vids
CMD ["go", "run", "main.go"]