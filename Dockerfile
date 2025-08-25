FROM golang:latest

WORKDIR /code
RUN apt-get update
RUN apt-get -y install python3
RUN apt-get -y install python3-setuptools
RUN apt-get -y install python3-pip
RUN apt-get install -y ffmpeg
RUN mkdir /usr/bin/yt-dlp
ENV PATH="/usr/bin/yt-dlp:$PATH"
ADD https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp /usr/bin/yt-dlp
RUN chmod a+rx /usr/bin/yt-dlp/yt-dlp
COPY . ./code
RUN go mod download
RUN mkdir temp_vids
CMD ["go", "run", "main.go"]