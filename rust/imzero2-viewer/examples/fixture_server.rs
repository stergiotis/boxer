//! Local codec smoke peer. Input is one independently decodable AU (not a container).
use anyhow::Result;
use futures_util::{SinkExt, StreamExt};
use imzero2_viewer::wire::{framed, pb, PREFIX_SESSION, PREFIX_VIDEO};
use prost::Message as _;
use tokio_tungstenite::tungstenite::Message;

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().collect();
    anyhow::ensure!(
        args.len() == 4,
        "usage: fixture_server PORT CODEC ACCESS_UNIT_FILE"
    );
    let port: u16 = args[1].parse()?;
    let codec = match args[2].as_str() {
        "h264" => "",
        "vp9" => "vp09.00.21.08",
        "av1" => "av01.0.04M.08",
        _ => anyhow::bail!("codec must be h264, vp9 or av1"),
    };
    let data = std::fs::read(&args[3])?;
    let listener = tokio::net::TcpListener::bind(("127.0.0.1", port)).await?;
    println!("listening {port}");
    let (tcp, _) = listener.accept().await?;
    let ws = tokio_tungstenite::accept_async(tcp).await?;
    let (mut tx, mut rx) = ws.split();
    let hello = pb::SessionControl {
        control: Some(pb::session_control::Control::Hello(pb::SessionHello {
            width_px: 320,
            height_px: 180,
            pixels_per_point: 1.0,
            cadence: 0,
            codec: codec.into(),
        })),
    };
    tx.send(Message::Binary(framed(PREFIX_SESSION, &hello).into()))
        .await?;
    let roster = pb::SessionControl {
        control: Some(pb::session_control::Control::Roster(pb::Roster {
            you_id: 1,
            you_role: 1,
            active_id: 1,
            ..Default::default()
        })),
    };
    tx.send(Message::Binary(framed(PREFIX_SESSION, &roster).into()))
        .await?;
    let mut interval = tokio::time::interval(std::time::Duration::from_millis(50));
    for index in 0..200u64 {
        tokio::select! {
            _=interval.tick()=>{
                let frame=pb::VideoChunk{frame_index:index,timestamp_micros:index*50_000,keyframe:true,data:data.clone()};
                if tx.send(Message::Binary(framed(PREFIX_VIDEO,&frame).into())).await.is_err(){return Ok(());}
            },
            incoming=rx.next()=>{
                let incoming = match incoming { Some(Ok(m)) => m, None | Some(Err(_)) => return Ok(()) };
                match incoming {
                    Message::Ping(p)=>tx.send(Message::Pong(p)).await?,
                    Message::Close(_)=>return Ok(()),
                    Message::Binary(b)=>{
                        println!("client message prefix={} bytes={}",b.first().copied().unwrap_or(0),b.len());
                        if b.first()==Some(&2) {println!("input {:?}",pb::InputEvent::decode(&b[1..])?);}
                    },
                    _=>{},
                }
            }
        }
    }
    anyhow::bail!("client did not finish within fixture budget")
}
