FROM alpine:latest

RUN mkdir -p /usr/share/zoneinfo/Asia/
COPY Shanghai /usr/share/zoneinfo/Asia/
RUN ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

WORKDIR /app/

# 拷贝构建好的二进制
COPY ./build/vst-metrics-hami-exporter /app/

EXPOSE 9808

# 启动服务
ENTRYPOINT ["./vst-metrics-hami-exporter"]

