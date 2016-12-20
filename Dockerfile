FROM scratch
MAINTAINER Andrew O'Neill <foolusion@gmail.com>
ADD rt53-updater /rt53-updater
ENTRYPOINT ["/rt53-updater"]
